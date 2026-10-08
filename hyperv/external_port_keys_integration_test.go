//go:build integration

package hyperv

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/r4sd/go-wsman/wsman"
)

// TestIntegration_ExternalAdapterBindingKeysResolve は #146 を実機で検証する。
//
// # なぜこの形で検証するか
//
// #146 は「`Msvm_ExternalEthernetPort` の参照が MOF のキーと一致していない」という話で、
// 以前は `CreationClassName` + `Name` を送っていた (`Name` はキーではない)。
//
// 🔴 **スイッチの作成成否では判定できない。** 検証環境の物理 NIC は既存スイッチに
// 束ねられており (`IsBound=true`)、束ねられた NIC への再バインドは
// **キーが正しくても間違っていても `ErrorCode=32773`** になる。
//
// そこで「作れるか」ではなく **「組み立てた参照がその NIC を指すか」** を直接見る。
// これは Get で読み取るだけなので未束縛の NIC が要らない。
//
// # 何を担保するか / しないか
//
//	担保する:   buildExternalAdapterBinding が組む HostResource のキーが、
//	            実機でその NIC を一意に解決する
//	担保しない: External スイッチが実際に作成できること・通信できること。
//	            そこは未束縛の物理 NIC が使える環境が必要 (#146 に残す)
func TestIntegration_ExternalAdapterBindingKeysResolve(t *testing.T) {
	client := getIntegrationClient(t)
	ctx := context.Background()

	ports, err := client.ListExternalEthernetPorts(ctx)
	if err != nil {
		t.Fatalf("ListExternalEthernetPorts: %v", err)
	}
	if len(ports) == 0 {
		t.Skip("ホストに Msvm_ExternalEthernetPort が無い")
	}

	var target *Msvm_ExternalEthernetPort
	for _, p := range ports {
		if p.ElementName != "" && p.DeviceID != "" && p.SystemName != "" {
			target = p
			break
		}
	}
	if target == nil {
		t.Fatalf("MOF キーが揃ったポートが無い (%d 件列挙)", len(ports))
	}
	t.Logf("対象ポート: ElementName=%q IsBound=%v", target.ElementName, target.IsBound)

	// --- 実装が組み立てた参照を取り出す ---
	embedded, err := client.buildExternalAdapterBinding(ctx, CreateSwitchOptions{
		Name:            "go-wsman-acctest-146-not-created",
		ExternalAdapter: target.ElementName,
	})
	if err != nil {
		t.Fatalf("buildExternalAdapterBinding: %v", err)
	}
	m := hostResourcePattern.FindStringSubmatch(embedded)
	if m == nil {
		t.Fatalf("HostResource を取り出せない:\n%s", embedded)
	}
	selectors := parseWMIObjectPathKeys(t, m[1])
	t.Logf("組み立てられたキー: %v", selectorNames(selectors))

	// --- 本題: そのキーで実機の NIC が解決するか ---
	inst, err := client.wsman.Get(ctx, msvmExternalEthernetPortURI, selectors...)
	if err != nil {
		t.Fatalf("組み立てたキーで Get できない (#146 のキー不一致が残っている): %v", err)
	}
	if got := inst.Property("DeviceID"); got != target.DeviceID {
		t.Errorf("解決したのは別のポート: DeviceID = %q, want %q", got, target.DeviceID)
	}
	if got := inst.Property("ElementName"); got != target.ElementName {
		t.Errorf("解決したのは別のポート: ElementName = %q, want %q", got, target.ElementName)
	}

	// --- 陰性対照: 旧実装のキーでは解決しないこと ---
	//
	// これが無いと「どんなキーでも解決する」のか「正しいキーだから解決した」のかが
	// 区別できない。旧形式が通ってしまうなら、この検査には #146 を落とす力が無い。
	_, oldErr := client.wsman.Get(ctx, msvmExternalEthernetPortURI,
		wsman.Selector{Name: "CreationClassName", Value: "Msvm_ExternalEthernetPort"},
		wsman.Selector{Name: "Name", Value: target.ElementName})
	if oldErr == nil {
		t.Fatalf("旧実装のキー (CreationClassName + Name) でも解決してしまった。" +
			"この検査には #146 を検出する力が無い")
	}
	// 🔴 **失敗の理由まで見る。** `err != nil` だけだと、接続断・認証切れ・値の形など
	// **キーと無関係な理由**でも陰性対照が「OK」になってしまう。
	// InvalidSelectors = 「そのセレクタではリソースを特定できない」= Name が
	// キーでないこと (#146 の本質) に対応する。
	var fault *wsman.Fault
	if !errors.As(oldErr, &fault) {
		t.Errorf("旧キーの失敗が WS-Man Fault でない (キーと無関係な理由で落ちている可能性): %v", oldErr)
	} else if !strings.HasSuffix(fault.Subcode, "InvalidSelectors") {
		t.Errorf("旧キーの失敗理由が InvalidSelectors でない (Subcode=%q)。"+
			"キー名が原因だという切り分けが崩れている", fault.Subcode)
	} else {
		t.Logf("陰性対照 OK: 旧キーは %s で失敗する", fault.Subcode)
	}
}
