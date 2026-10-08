package hyperv

import (
	"reflect"
	"strings"
	"testing"
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
// **短い値も除外しない。** wsman/record.go の scrub は `boundedByNonAlnum` による
// **単語境界付き**の置換で、部分一致ではない (`tf` を足しても `tfvars` は置換されない)。
// 閾値で弾くと、短いユーザー名が伏せ字にも verifyRecorded の検査対象にも入らず
// **無警告で記録に残る** — #188 が潰したかった「静かに漏れる」型そのもの。
// 過剰に伏せた場合は記録が目で見て壊れるので気付ける。**見える失敗を選ぶ。**
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
