package wsman

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// 録音 (#157)。
//
// このリポジトリは「手書き golden が実機に無い挙動を仕様として固定する」事故を
// 7 回繰り返している。警告文と規約は 1 度も効かなかった (5 回目は「4 回繰り返した」と
// CLAUDE.md に書いた同じセッションが数時間後に起こしている)。
//
// 効くのは「正しい道を間違った道より安くする」ことなので、実機応答の採取を
// 1 コマンドにする。
//
// **go-vcr を一度採用して外した経緯**: 録音・再生の OSS として go-vcr v4 を入れたが、
// 本リポジトリの単体テストは ParsePullResponse(loadGolden(...)) 型で、HTTP 会話の
// 再生を必要としない。再生を使おうとすると WS-Man 固有の照合 (全リクエストが同じ URL への
// POST で、MessageID は毎回変わる) を自作することになり、「肩に乗ったつもりで肩の上で
// 別の車輪を作る」状態だった。応答の採取だけに絞り、依存を外した。
// 会話の再生が要る形のテストが出てきたら改めて検討する。

// recordedByMarker は録音器が書いたファイルであることを示す印。
// 関所 (internal/guard) がこの行と sha256 を確かめる。
const recordedByMarker = "recorded-by: go-wsman/wsman.WithRecorder"

// anonHostPlaceholder は伏せた接続先ホストの代わりに入る名前。
const anonHostPlaceholder = "hyperv-host.example.invalid"

var (
	// anonGUIDPattern は CIM が返す GUID。VM / スナップショット / リソースの識別子として
	// 応答中に頻出し、そのまま公開リポジトリへ置けない。
	anonGUIDPattern = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

	// privateIPPattern は RFC 1918 のアドレス。公開リポジトリの CI (no-private-addresses) が
	// 落とす対象なので、指定が無くても伏せる。
	privateIPPattern = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)

	// recordedHeaderPattern は録音ファイル先頭のヘッダコメント。
	recordedHeaderPattern = regexp.MustCompile(`(?s)\A<!--.*?-->\n`)

	// recordedHashPattern はヘッダ中の本文ハッシュ。
	recordedHashPattern = regexp.MustCompile(`sha256:\s*([0-9a-f]{64})`)
)

// anonymizer は実環境の識別子をプレースホルダへ決定的に写す。
//
// 決定的にするのは、再録音したときの差分を読めるようにするため。
// ランダムだと毎回全行が変わって「実機の挙動が変わったのか、採り直しただけか」が
// 区別できなくなる。
type anonymizer struct {
	mu       sync.Mutex
	hosts    []string
	literals []string
	replaced map[string]string
	counters map[string]int // 種別ごと。共有すると IP の採番が 255 を超えて不正な値になる
}

func newAnonymizer(endpoint string, literals []string) *anonymizer {
	a := &anonymizer{replaced: make(map[string]string), counters: make(map[string]int)}
	for _, lit := range literals {
		if lit != "" {
			a.literals = append(a.literals, lit)
		}
	}
	if u, err := url.Parse(endpoint); err == nil {
		if u.Host != "" {
			a.hosts = append(a.hosts, u.Host)
		}
		if h := u.Hostname(); h != "" && h != u.Host {
			a.hosts = append(a.hosts, h)
		}
	}
	// 長い方から置換しないと "host:port" が "host" で先に壊れる。
	sort.Slice(a.hosts, func(i, j int) bool { return len(a.hosts[i]) > len(a.hosts[j]) })
	sort.Slice(a.literals, func(i, j int) bool { return len(a.literals[i]) > len(a.literals[j]) })
	return a
}

// docIPRanges は伏せた IP の行き先。RFC 5737 の文書用レンジ。
// 1 レンジ 254 個で足りなくなったら次へ繰り上げる。
var docIPRanges = []string{"203.0.113", "198.51.100", "192.0.2"}

// placeholderFor は同じ入力に必ず同じ置換結果を返す。
//
// 採番は**種別ごと**に持つ。1 つのカウンタを共有すると、GUID を 300 件含む応答の後に
// IP が来たときに 203.0.113.301 のような不正なアドレスを書いてしまう
// (= 録音器自身が実機に無い値を作る)。
func (a *anonymizer) placeholderFor(prefix, s string) string {
	key := prefix + "\x00" + strings.ToLower(s)
	if v, ok := a.replaced[key]; ok {
		return v
	}
	a.counters[prefix]++
	n := a.counters[prefix]

	var v string
	switch prefix {
	case "guid":
		// 出力は RFC 4122 の v4 形に似せる。GUID を期待するパーサを壊さないため。
		v = fmt.Sprintf("00000000-0000-4000-8000-%012x", n)
	case "ip":
		idx := (n - 1) / 254
		if idx >= len(docIPRanges) {
			// 文書用レンジを使い切った。正しくない値を書くより落とす。
			idx = len(docIPRanges) - 1
		}
		v = fmt.Sprintf("%s.%d", docIPRanges[idx], (n-1)%254+1)
	default:
		v = fmt.Sprintf("%s-%d", prefix, n)
	}
	a.replaced[key] = v
	return v
}

// xmlEscapeForScrub は XML テキストとしてエスケープされた形を返す。
func xmlEscapeForScrub(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// replaceFold は大文字小文字を無視して old を replacement に置換する。
//
// 小文字化した文字列のバイト位置を原文へ当てる実装にしてはいけない。
// ToLower はバイト長を変える文字があり (İ → i̇ は 2 バイト → 3 バイト)、
// 位置がずれて XML のタグを破壊し、伏せたい値の一部が残る。
func replaceFold(s, old, replacement string) string {
	if old == "" {
		return s
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(old))
	if err != nil {
		return s
	}
	return re.ReplaceAllLiteralString(s, replacement)
}

// scrubVariants は 1 つの値について、XML 上に現れうる表記ゆれを全部伏せる。
//
// 生の文字列だけを置換すると取りこぼす。実機の応答では
//
//   - XML エスケープされる (R&D-vm → R&amp;D-vm)
//   - 大文字小文字が変わる (CIM の Name は大文字、endpoint は小文字で渡される等)
//
// のどちらも起きる。取りこぼすと「伏せたつもり」で公開リポジトリへ出る。
func scrubVariants(s, value, replacement string) string {
	for _, v := range []string{value, xmlEscapeForScrub(value)} {
		if v == "" {
			continue
		}
		s = replaceFold(s, v, replacement)
	}
	return s
}

func (a *anonymizer) scrub(s string) string {
	if s == "" {
		return s
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, h := range a.hosts {
		s = scrubVariants(s, h, anonHostPlaceholder)
	}
	for _, lit := range a.literals {
		s = scrubVariants(s, lit, a.placeholderFor("scrubbed", lit))
	}
	// 複数のアドレスを 1 つに潰すと区別が要るテストで使えないので、決定的に採番する。
	s = privateIPPattern.ReplaceAllStringFunc(s, func(ip string) string {
		return a.placeholderFor("ip", ip)
	})
	return anonGUIDPattern.ReplaceAllStringFunc(s, func(g string) string {
		return a.placeholderFor("guid", g)
	})
}

// recorder は実機応答を匿名化して XML ファイルへ落とす RoundTripper。
//
// 応答本文だけを残す。リクエストとの対応やヘッダは保存しない
// (単体テストは応答 XML をパースするだけなので要らない)。
type recorder struct {
	next  http.RoundTripper
	anon  *anonymizer
	dir   string
	name  string
	mu    sync.Mutex
	seq   int
	files []string
	errs  []error
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := r.next.RoundTrip(req)
	if err != nil || resp == nil || resp.Body == nil {
		return resp, err
	}
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		// 読めなかったことを握り潰すと、上位のパースエラーに化けて原因を誤らせる。
		return resp, readErr
	}
	if writeErr := r.write(body); writeErr != nil {
		r.mu.Lock()
		r.errs = append(r.errs, writeErr)
		r.mu.Unlock()
	}
	return resp, err
}

func (r *recorder) write(body []byte) error {
	// SOAP 以外 (認証ハンドシェイクの空応答等) は残さない。
	if !bytes.Contains(body, []byte("Envelope")) {
		return nil
	}
	scrubbed := r.anon.scrub(string(body))
	// 置換が XML の構造を壊していないか確かめる。壊れたものを fixture にすると、
	// 録音器自身が「実機に無い形」を作ることになる。
	if err := wellFormedXML(scrubbed); err != nil {
		return fmt.Errorf("匿名化した結果が XML として壊れている: %w", err)
	}

	r.mu.Lock()
	r.seq++
	seq := r.seq
	r.mu.Unlock()

	path := filepath.Join(r.dir, fmt.Sprintf("%s-%03d.xml", r.name, seq))
	sum := sha256.Sum256([]byte(scrubbed))
	header := fmt.Sprintf("<!--\n  %s\n  recorded-at: %s\n  sha256: %s\n\n"+
		"  実機から録音した応答。手で編集しない (sha256 が合わなくなり CI が落ちる)。\n"+
		"  値を変えたいなら合成として testdata/synthetic/ へ置き derived-from: を書くこと。\n-->\n",
		recordedByMarker, time.Now().UTC().Format(time.RFC3339), hex.EncodeToString(sum[:]))

	if err := os.WriteFile(path, []byte(header+scrubbed), 0o600); err != nil {
		return fmt.Errorf("録音の書き出しに失敗 (%s): %w", path, err)
	}
	r.mu.Lock()
	r.files = append(r.files, path)
	r.mu.Unlock()
	return nil
}

// wellFormedXML は文字列が整形式 XML かを確かめる。
func wellFormedXML(s string) error {
	dec := xml.NewDecoder(strings.NewReader(s))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// WithRecorder は実機の応答を匿名化した XML として dir へ書き出す。
//
// ファイル名は <name>-001.xml のような連番。先頭に録音器が書いた印と本文の
// sha256 が入り、関所 (internal/guard) がそれを確かめる。
//
// 匿名化するもの: GUID / 接続先ホスト / プライベート IP / [WithRecorderScrub] で
// 指定した文字列。いずれも XML エスケープ後の形と大文字小文字の違いを含めて伏せる。
//
// 録音を確定させるには必ず [Client.StopRecording] を呼ぶこと。
func WithRecorder(dir, name string) ClientOption {
	return func(c *Client) {
		c.recordDir = dir
		c.recordName = name
	}
}

// WithRecorderScrub は録音時に伏せる文字列を追加する。
//
// GUID・接続先ホスト・プライベート IP はパターンで拾えるが、VM の表示名や
// ホストのコンピュータ名は**任意のユーザーデータ**なので拾えない。
// 公開リポジトリへ置くなら明示すること。
//
// 指定した値が書き出したファイルに残っていた場合、[Client.StopRecording] が
// エラーを返す (静かに漏らさない)。
func WithRecorderScrub(values ...string) ClientOption {
	return func(c *Client) {
		c.recordScrub = append(c.recordScrub, values...)
	}
}

// StopRecording は録音を確定し、書き出したファイルを検査する。
// 録音していない Client では何もしない。
func (c *Client) StopRecording() error {
	if c.recorder == nil {
		return nil
	}
	rec := c.recorder
	c.recorder = nil

	rec.mu.Lock()
	files := append([]string(nil), rec.files...)
	errs := append([]error(nil), rec.errs...)
	rec.mu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("録音中にエラーがあった: %w", errs[0])
	}

	// 匿名化を「したつもり」で終わらせない。書いたファイルそのものを読み返して
	// 検査する。実装の取りこぼし (別の表記で載った等) はここで初めて分かる。
	for _, path := range files {
		raw, err := os.ReadFile(path) //#nosec G304 -- 自分が直前に書いたファイル
		if err != nil {
			return fmt.Errorf("録音したファイルを読み返せない (%s): %w", path, err)
		}
		if err := verifyRecorded(raw, c.recordScrub, c.endpoint); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return nil
}

// verifyRecorded は録音ファイルに実環境の識別子が残っていないか、
// 本文がヘッダの sha256 と一致するかを検査する。
//
// 匿名化の実装を信頼せず、出力を直接見る。漏れたまま公開リポジトリへ commit するより、
// 録音し直す方が安いので fail-loud にする。
func verifyRecorded(content []byte, scrub []string, endpoint string) error {
	if err := VerifyRecordedHash(content); err != nil {
		return err
	}
	body := stripRecordedHeader(string(content))
	lower := strings.ToLower(body)
	var leaks []string

	leaks = append(leaks, privateIPPattern.FindAllString(body, -1)...)
	for _, lit := range scrub {
		if lit == "" {
			continue
		}
		for _, v := range []string{lit, xmlEscapeForScrub(lit)} {
			if strings.Contains(lower, strings.ToLower(v)) {
				leaks = append(leaks, lit)
			}
		}
	}
	if endpoint != "" {
		if u, err := url.Parse(endpoint); err == nil {
			if h := u.Hostname(); h != "" && h != anonHostPlaceholder &&
				strings.Contains(lower, strings.ToLower(h)) {
				leaks = append(leaks, h)
			}
		}
	}
	if len(leaks) == 0 {
		return nil
	}
	return fmt.Errorf("匿名化されていない値が残っている: %s", strings.Join(uniqueStrings(leaks), ", "))
}

// VerifyRecordedHash は録音ファイルのヘッダにある sha256 と本文が一致するか確かめる。
//
// 録音した後に値を「アサーションに合わせて調整」する改変を検出するためのもの
// (実際にその痕跡が残っている既存ファイルがある)。関所からも使う。
func VerifyRecordedHash(content []byte) error {
	s := string(content)
	if !strings.Contains(s, recordedByMarker) {
		return fmt.Errorf("録音器が書いた印 (%q) が無い", recordedByMarker)
	}
	m := recordedHashPattern.FindStringSubmatch(s)
	if m == nil {
		return fmt.Errorf("ヘッダに sha256 が無い")
	}
	sum := sha256.Sum256([]byte(stripRecordedHeader(s)))
	if got := hex.EncodeToString(sum[:]); got != m[1] {
		return fmt.Errorf("本文がヘッダの sha256 と一致しない (録音後に編集された)。" +
			"録音し直すか、合成として testdata/synthetic/ へ移すこと")
	}
	return nil
}

// stripRecordedHeader は先頭のヘッダコメントを取り除いた本文を返す。
func stripRecordedHeader(s string) string {
	return recordedHeaderPattern.ReplaceAllString(s, "")
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// attachRecorder は transport 確定後に録音器をチェーンへ差し込む。
//
// NewClient の最後で呼ぶこと。WithInsecureSkipVerify が内側の *http.Transport を
// 型アサーションで取り出すため、それより先に包むと TLS 設定が効かなくなる。
func attachRecorder(c *Client) error {
	if c.recordDir == "" {
		return nil
	}
	if c.transport == nil || c.transport.httpClient == nil {
		return fmt.Errorf("WithRecorder: transport が確定していない")
	}
	if err := os.MkdirAll(c.recordDir, 0o750); err != nil {
		return fmt.Errorf("WithRecorder: 録音先を作れない (%s): %w", c.recordDir, err)
	}
	name := c.recordName
	if name == "" {
		name = "recorded"
	}
	rec := &recorder{
		next: c.transport.httpClient.Transport,
		anon: newAnonymizer(c.endpoint, c.recordScrub),
		dir:  c.recordDir,
		name: name,
	}
	if rec.next == nil {
		rec.next = http.DefaultTransport
	}
	c.transport.httpClient.Transport = rec
	c.recorder = rec
	return nil
}
