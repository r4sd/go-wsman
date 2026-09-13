package wsman

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// 録音 (#157)。
//
// このリポジトリは「手書き golden が実機に無い挙動を仕様として固定する」事故を
// 7 回繰り返している。警告文と規約は 1 度も効かなかった (5 回目は「4 回繰り返した」と
// CLAUDE.md に書いた同じセッションが数時間後に起こしている)。
//
// 効くのは「正しい道を間違った道より安くする」ことだけなので、実機応答の採取を
// 1 コマンドにする。録音・再生そのものは go-vcr に任せ、ここは
//
//   - 既存の RoundTripper チェーン (NTLM 等) の上に録音器を差す
//   - 保存前に実環境の識別子を落とす
//
// の 2 点だけを持つ。

// anonymizer は実環境の識別子をプレースホルダへ決定的に写す。
//
// 決定的にするのは、再録音したときの差分を読めるようにするため。
// ランダムだと毎回全行が変わって「実機の挙動が変わったのか、採り直しただけか」が
// 区別できなくなる。
type anonymizer struct {
	mu       sync.Mutex
	hosts    []string          // 伏せる接続先ホスト (endpoint 由来)
	literals []string          // 伏せる任意の文字列 (VM 表示名・コンピュータ名など)
	replaced map[string]string // 元の値 → プレースホルダ
}

// anonGUIDPattern は CIM が返す GUID。VM / スナップショット / リソースの識別子として
// 応答中に頻出し、そのまま公開リポジトリへ置けない。
var anonGUIDPattern = regexp.MustCompile(`[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}`)

// privateIPPattern は RFC 1918 のアドレス。公開リポジトリの CI (no-private-addresses) が
// 落とす対象なので、指定が無くても伏せる。
var privateIPPattern = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)

// anonHostPlaceholder は伏せた接続先ホストの代わりに入る名前。
// 検証側でも使うので定数にしてある。
const anonHostPlaceholder = "hyperv-host.example.invalid"

func newAnonymizer(endpoint string, literals []string) *anonymizer {
	a := &anonymizer{replaced: make(map[string]string)}
	// 長い方から置換する (短い方が先に当たって部分文字列を壊すのを避ける)。
	a.literals = append(a.literals, literals...)
	sort.Slice(a.literals, func(i, j int) bool { return len(a.literals[i]) > len(a.literals[j]) })
	if u, err := url.Parse(endpoint); err == nil {
		if u.Host != "" {
			a.hosts = append(a.hosts, u.Host)
		}
		if u.Hostname() != "" && u.Hostname() != u.Host {
			a.hosts = append(a.hosts, u.Hostname())
		}
	}
	// 長い方から置換しないと "host:port" が "host" で先に壊れる。
	sort.Slice(a.hosts, func(i, j int) bool { return len(a.hosts[i]) > len(a.hosts[j]) })
	return a
}

// placeholderFor は同じ入力に必ず同じプレースホルダを返す。
// 出力は RFC 4122 の v4 形に似せてあるので、GUID を期待するパーサを壊さない。
func (a *anonymizer) placeholderFor(s string) string {
	if v, ok := a.replaced[s]; ok {
		return v
	}
	v := fmt.Sprintf("00000000-0000-4000-8000-%012x", len(a.replaced)+1)
	a.replaced[s] = v
	return v
}

func (a *anonymizer) scrub(s string) string {
	if s == "" {
		return s
	}
	for _, h := range a.hosts {
		s = strings.ReplaceAll(s, h, anonHostPlaceholder)
	}
	for i, lit := range a.literals {
		s = strings.ReplaceAll(s, lit, fmt.Sprintf("scrubbed-%d", i+1))
	}
	s = privateIPPattern.ReplaceAllString(s, "203.0.113.1")
	return anonGUIDPattern.ReplaceAllStringFunc(s, a.placeholderFor)
}

// hook は go-vcr の BeforeSaveHook に渡す。ディスクへ書く直前に通るので、
// ここを通らずにカセットが保存されることは無い。
func (a *anonymizer) hook(i *cassette.Interaction) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	i.Request.Body = a.scrub(i.Request.Body)
	i.Request.URL = a.scrub(i.Request.URL)
	i.Request.Host = a.scrub(i.Request.Host)
	i.Response.Body = a.scrub(i.Response.Body)

	// 資格情報がヘッダに載る (NTLM の WWW-Authenticate / Authorization)。
	// 値を残す理由が無いので落とす。
	for _, h := range []map[string][]string{i.Request.Headers, i.Response.Headers} {
		for k, vs := range h {
			switch {
			case strings.EqualFold(k, "Authorization"), strings.EqualFold(k, "WWW-Authenticate"):
				h[k] = []string{"REDACTED"}
			default:
				for n := range vs {
					vs[n] = a.scrub(vs[n])
				}
			}
		}
	}
	return nil
}

// WithRecorder は実機とのやり取りを go-vcr のカセットへ録音する。
//
// cassettePath には拡張子を付けない (go-vcr が ".yaml" を足す)。
// 保存前に GUID と接続先ホストをプレースホルダへ決定的に置換し、
// Authorization / WWW-Authenticate は落とす。
//
// 録音を確定させるには必ず [Client.StopRecording] を呼ぶこと。
// 呼ばないとカセットはディスクに書かれない。
func WithRecorder(cassettePath string) ClientOption {
	return func(c *Client) {
		c.recordCassette = cassettePath
	}
}

// WithRecorderScrub は録音時に伏せる文字列を追加する。
//
// GUID・接続先ホスト・プライベート IP・資格情報は指定しなくても伏せるが、
// VM の表示名やホストのコンピュータ名は**任意のユーザーデータ**なので
// パターンでは拾えない。公開リポジトリへ置くカセットでは明示すること。
//
// 指定した値が保存後のカセットに残っていた場合、[Client.StopRecording] が
// エラーを返す (静かに漏らさない)。
func WithRecorderScrub(values ...string) ClientOption {
	return func(c *Client) {
		c.recordScrub = append(c.recordScrub, values...)
	}
}

// StopRecording は録音を確定してカセットをディスクへ書き出す。
// 録音していない Client では何もしない。
func (c *Client) StopRecording() error {
	if c.recorder == nil {
		return nil
	}
	rec := c.recorder
	c.recorder = nil
	if err := rec.Stop(); err != nil {
		return fmt.Errorf("failed to stop recorder: %w", err)
	}

	// 匿名化を「したつもり」で終わらせない。書いたファイルそのものを読み返して
	// 検査する。実装の取りこぼし (別のエンコードで載った等) はここで初めて分かる。
	path := c.recordCassette + ".yaml"
	raw, err := os.ReadFile(path) //#nosec G304 -- 呼び出し側が指定した録音先
	if err != nil {
		return fmt.Errorf("録音したカセットを読み返せない (%s): %w", path, err)
	}
	if err := verifyCassetteScrubbed(raw, c.recordScrub, c.endpoint); err != nil {
		return fmt.Errorf("カセット %s: %w", path, err)
	}
	return nil
}

// verifyCassetteScrubbed は保存済みカセットに実環境の識別子が残っていないか検査する。
//
// 匿名化の実装を信頼せず、出力を直接見る。漏れたまま公開リポジトリへ commit するより、
// 録音し直す方が安いので fail-loud にする。
func verifyCassetteScrubbed(content []byte, scrub []string, endpoint string) error {
	s := string(content)
	var leaks []string

	if m := privateIPPattern.FindAllString(s, -1); len(m) > 0 {
		leaks = append(leaks, m...)
	}
	for _, lit := range scrub {
		if lit != "" && strings.Contains(s, lit) {
			leaks = append(leaks, lit)
		}
	}
	if endpoint != "" {
		if u, err := url.Parse(endpoint); err == nil && u.Hostname() != "" &&
			u.Hostname() != anonHostPlaceholder && strings.Contains(s, u.Hostname()) {
			leaks = append(leaks, u.Hostname())
		}
	}
	if len(leaks) == 0 {
		return nil
	}
	return fmt.Errorf("匿名化されていない値が残っている: %s", strings.Join(uniqueStrings(leaks), ", "))
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
	if c.recordCassette == "" {
		return nil
	}
	if c.transport == nil || c.transport.httpClient == nil {
		return fmt.Errorf("WithRecorder: transport が確定していない")
	}

	anon := newAnonymizer(c.endpoint, c.recordScrub)
	rec, err := recorder.New(c.recordCassette,
		recorder.WithMode(recorder.ModeRecordOnly),
		recorder.WithRealTransport(c.transport.httpClient.Transport),
		recorder.WithHook(anon.hook, recorder.BeforeSaveHook),
	)
	if err != nil {
		return fmt.Errorf("WithRecorder: %w", err)
	}
	c.transport.httpClient.Transport = rec
	c.recorder = rec
	return nil
}
