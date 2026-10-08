package hyperv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/r4sd/go-wsman/wsman"
)

// scrubNamesForUser は接続ユーザー名から伏せるべき文字列を列挙する。
//
// 🔴 **Job 系のクラス (Msvm_StorageJob / Msvm_ConcreteJob) は Owner に
// "<ホスト名>\<ユーザー名>" を載せる** (#188)。ホスト名は実機から集めた名前で置換されるが、
// ユーザー名は**こちらが渡している値**なので「実機から集める」方針から漏れていた。
//
// `DOMAIN\user` 形式で渡される場合もあるので、全体と分割した各要素を対象にする
// (Owner には "<ホスト名>\<ユーザー名>" の形で載るため、ユーザー名単体が要る)。
//
// **短い値も除外しない。** 閾値で弾くと、短いユーザー名が伏せ字にも
// verifyRecorded の検査対象にも入らず **無警告で記録に残る** —
// #188 が潰したかった「静かに漏れる」型そのもの。**見える失敗を選ぶ。**
//
// 🔴 **この値は経路が 2 本あり、挙動が違う** (wsman/record.go):
//
//	scrubWordBounded  置換側。`boundedByNonAlnum` による**単語境界付き**。
//	                  `tf` を足しても `tfvars` は置換されない
//	verifyRecorded    保存後の検品側。`strings.Contains` の**部分一致**
//
// 検品の方が厳しいので fail-closed (漏れるのではなく弾かれる)。ただし
// **過剰に伏せた時の実際の帰結は「記録が目で見て壊れる」ではなく
// 「StopRecording が必ずエラーになり記録できない」**。たとえば `tf` を
// 伏せ字に入れると `C:\tfvars\...` の `tfvars` は置換されないまま残り、
// 検品が部分一致でそれを漏れと判定する。実機の WS-Man 応答は `xmlns:a` を
// 常に含むので、ユーザー名が `a` だと記録不能。
//
// この非対称そのものは #191 で追跡する (踏んだ実例は無いので優先度低)。
//
// ⚠️ UPN 形式 (`user@domain`) は `\` で分割できないため全体のみが対象になる。
// その形で NTLM 認証したときに Owner が `<ホスト名>\<user>` で載ると当たらない
// (実機未確認)。使う場合は `WSMAN_USERNAME` にアカウント名側を渡すこと。
//
// この関数と単体テストは **integration タグ無しのファイル**に置く。
// タグ付きファイルに置くと通常のテスト実行でコンパイルされず、変異が検出できない。
func scrubNamesForUser(user string) []string {
	user = strings.TrimSpace(user)
	if user == "" {
		return nil
	}
	out := []string{user}
	for _, part := range strings.Split(user, `\`) {
		part = strings.TrimSpace(part)
		if part == "" || part == user {
			continue
		}
		out = append(out, part)
	}
	return out
}

// TestScrubNamesForUser は伏せ字に渡す候補の導出を固定する (#188)。
func TestScrubNamesForUser(t *testing.T) {
	for _, tt := range []struct {
		name string
		user string
		want []string
	}{
		{"単体", "svc-hyperv", []string{"svc-hyperv"}},
		{"DOMAIN\\user は全体と各要素", `CORP\svc-hyperv`, []string{`CORP\svc-hyperv`, "CORP", "svc-hyperv"}},
		// 🔴 短い値も落とさない。閾値で弾くと無警告で記録に残る (#188)。
		{"2 文字でも落とさない", "tf", []string{"tf"}},
		{"1 文字でも落とさない", "a", []string{"a"}},
		{"空は nil", "", nil},
		{"空白だけは nil", "   ", nil},
		{"前後の空白は落とす", "  svc  ", []string{"svc"}},
		// UPN は \ で分割できないので全体のみ。doc に限界として明記してある。
		{"UPN は全体のみ", "svc@corp.local", []string{"svc@corp.local"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := scrubNamesForUser(tt.user)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("scrubNamesForUser(%q) = %#v, want %#v", tt.user, got, tt.want)
			}
		})
	}
}

// discoverScrubNames は記録前に実機から「伏せるべき名前」を集める (#157)。
//
// VM の表示名とホストのコンピュータ名は任意のユーザーデータで、GUID のように
// パターンで拾えない。環境変数で人が渡す形にすると設定し忘れで静かに漏れるので、
// 記録用とは別の Client で 1 回列挙して自動で集める。
//
// 集め損ねた名前があってもここでは落とさない。最終的な安全網は StopRecording の
// 保存後検証で、そこには集めた名前が渡る。
func discoverScrubNames(t *testing.T, endpoint string, baseOpts []wsman.ClientOption) []string {
	t.Helper()
	// 🔴 **接続ユーザー名は probe の成否に関わらず先に足す (#188)。**
	// これは実機から集める値ではなく、こちらが渡している値。下の early return の
	// 位置に置くと、probe が失敗したときに伏せ字にも StopRecording の保存後検証にも
	// 入らず**無警告で記録に残る** — #188 が潰したかった「静かに漏れる」型そのもの。
	//
	// Job 系のクラス (Msvm_StorageJob / Msvm_ConcreteJob) は Owner に
	// "<ホスト名>\<ユーザー名>" を載せる。ホスト名は下で集めた名前で置換される。
	names := scrubNamesForUser(os.Getenv("WSMAN_USERNAME"))

	// baseOpts は記録オプションを含まない。この列挙自体は記録に載せない。
	probe, err := NewClient(endpoint, baseOpts...)
	if err != nil {
		t.Logf("⚠️ 実機からの名前収集に失敗 (Client 作成): %v。ユーザー名のみ伏せる", err)
		return names
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// ここだけはホストを含む素の列挙を使う。ホストのコンピューター名も伏せる対象で、
	// ListComputerSystems (ホスト除外済み) を使うと実機の記録に素のホスト名が残る (#139)。
	systems, err := probe.listComputerSystemsIncludingHost(ctx)
	if err != nil {
		t.Logf("⚠️ 実機からの名前収集に失敗 (listComputerSystemsIncludingHost): %v。ユーザー名のみ伏せる", err)
		return names
	}
	for _, cs := range systems {
		if cs.ElementName != "" {
			names = append(names, cs.ElementName)
		}
	}
	// 仮想スイッチの表示名も実環境の名前。応答に素で載る。
	if switches, err := probe.ListVirtualEthernetSwitches(ctx); err != nil {
		t.Logf("⚠️ スイッチ名の収集に失敗: %v", err)
	} else {
		for _, sw := range switches {
			if sw.ElementName != "" {
				names = append(names, sw.ElementName)
			}
		}
	}
	t.Logf("記録時に伏せる名前を %d 件収集した", len(names))
	return names
}

// TestDiscoverScrubNames_KeepsUsernameWhenProbeFails は実機への probe が失敗しても
// 接続ユーザー名が伏せ字リストに残ることを検証する (#188)。
//
// 🔴 **これが無いと早期 return の修正を戻しても誰も気付かない。** Fable のレビューで、
// `names := scrubNamesForUser(...)` を early return の後ろに戻す変異が
// **緑のまま生き残る**ことが分かった (discoverScrubNames が integration タグ配下に
// あり、通常の `go test ./...` からは誰も呼んでいなかった)。
//
// probe が失敗した記録だけユーザー名が素通りする経路で、#188 が潰したかった
// 「静かに漏れる」型そのもの。
func TestDiscoverScrubNames_KeepsUsernameWhenProbeFails(t *testing.T) {
	// 何を投げても 500 を返す = probe は必ず失敗する。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	t.Setenv("WSMAN_USERNAME", `CORP\svc-hyperv`)
	got := discoverScrubNames(t, server.URL, nil)

	for _, want := range []string{`CORP\svc-hyperv`, "CORP", "svc-hyperv"} {
		if !containsString(got, want) {
			t.Errorf("probe 失敗時の伏せ字リストに %q が無い: %v\n"+
				"  ユーザー名の append が early return の後ろに戻っていないか確認すること", want, got)
		}
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
