package hyperv

import (
	"context"
	"strings"
	"testing"
)

// TestInstallDateSuggestsHost は第 2 の材料 (InstallDate) の読み方を固定する (#185)。
//
// MOF は管理 OS で Null と書いている。struct は string なので Null は "" で届く。
func TestInstallDateSuggestsHost(t *testing.T) {
	tests := []struct {
		name string
		cs   Msvm_ComputerSystem
		want bool
	}{
		{"空文字 = Null = ホスト", Msvm_ComputerSystem{InstallDate: ""}, true},
		{"空白だけも Null 扱い", Msvm_ComputerSystem{InstallDate: "   "}, true},
		{"値があれば VM", Msvm_ComputerSystem{InstallDate: "2026-10-08T15:15:36.68424Z"}, false},
		{"0 埋めの日時でも値は値", Msvm_ComputerSystem{InstallDate: "1601-01-01T00:00:00Z"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cs.installDateSuggestsHost(); got != tt.want {
				t.Errorf("installDateSuggestsHost() = %v, want %v (InstallDate=%q)",
					got, tt.want, tt.cs.InstallDate)
			}
		})
	}
}

// TestObserveHostSignalDisagreement は食い違いの検出が**両方向で空振りしない**ことを示す (#185)。
//
// 🔴 この検出器は「食い違いが無い」ことを確かめるのに使う。空振りする実装
// (常に空を返す) でも記録に対しては緑になるので、先にここで落とせることを固定する。
func TestObserveHostSignalDisagreement(t *testing.T) {
	const guid = "A1B2C3D4-1111-2222-3333-444455556666"

	// 一致しているもの: ホスト (Name が GUID でない + InstallDate 空) と
	// VM (Name が GUID + InstallDate あり)。
	agreeing := []*Msvm_ComputerSystem{
		{Name: "WIN-HYPERVHOST", InstallDate: ""},
		{Name: guid, InstallDate: "2026-10-08T15:15:36Z"},
	}
	if got := observeHostSignalDisagreement(agreeing); len(got) != 0 {
		t.Errorf("一致しているのに %d 件を食い違いと報告した", len(got))
	}

	// 食い違い① Name はホストと言うが InstallDate は VM と言う
	// (= MOF どおり GUID を返さないホストで、かつ InstallDate が埋まっている実機)
	nameHostDateVM := &Msvm_ComputerSystem{Name: "WIN-HYPERVHOST", InstallDate: "2026-10-08T15:15:36Z"}
	// 食い違い② Name は VM と言うが InstallDate はホストと言う
	// (= InstallDate を返さない VM。AND に締めても落ちないが、OR なら消える側)
	nameVMDateHost := &Msvm_ComputerSystem{Name: guid, InstallDate: ""}

	for _, c := range []struct {
		why string
		cs  *Msvm_ComputerSystem
	}{
		{"Name=ホスト / InstallDate=VM", nameHostDateVM},
		{"Name=VM / InstallDate=ホスト", nameVMDateHost},
	} {
		got := observeHostSignalDisagreement(append(append([]*Msvm_ComputerSystem{}, agreeing...), c.cs))
		if len(got) != 1 {
			t.Errorf("%s: 食い違いを %d 件検出 (want 1)", c.why, len(got))
			continue
		}
		if got[0] != c.cs {
			t.Errorf("%s: 別のインスタンスを食い違いとして返した (Name=%q)", c.why, got[0].Name)
		}
	}
}

// TestHostSignalsAgreeOnRecordedInstances は、**実機記録すべて**で Name 由来の判定と
// InstallDate 由来の判定が一致することを固定する (#185)。
//
// #185 を締める (AND にする) 判断には「2 つが食い違う実機があるか」の観測が要る。
// 判定を変えずに観測を貯める入口がこれ。記録を足したとき (別ホスト・別バージョン・
// 別ロケール) に食い違えばここが落ちて気付ける。
//
// 🔴 **今の記録は実機 1 台 ・ VM 4 台だけ。** 「一致する」と言えるのはその範囲に限る。
func TestHostSignalsAgreeOnRecordedInstances(t *testing.T) {
	var bodies []string
	server := newSequenceServer(t, recordedComputerSystemSequence(t), &bodies)
	defer server.Close()

	client, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	all, err := client.listComputerSystemsIncludingHost(context.Background())
	if err != nil {
		t.Fatalf("listComputerSystemsIncludingHost: %v", err)
	}

	// 🔴 空振り防止。0 件や 1 件だと「食い違いが無い」は何も言っていない。
	if len(all) != 5 {
		t.Fatalf("記録から %d 件しか読めていない (want 5 = ホスト 1 + VM 4)", len(all))
	}
	byName, byDate := 0, 0
	for _, cs := range all {
		if cs.IsHostComputerSystem() {
			byName++
		}
		if cs.installDateSuggestsHost() {
			byDate++
		}
	}
	if byName != 1 || byDate != 1 {
		t.Fatalf("ホストと見るのが Name 由来 %d 件 / InstallDate 由来 %d 件 (want 1 / 1)", byName, byDate)
	}

	if dis := observeHostSignalDisagreement(all); len(dis) != 0 {
		for _, cs := range dis {
			t.Errorf("2 つの材料が食い違う実機インスタンスがある: Name=%q InstallDate=%q "+
				"(Name 由来=%v / InstallDate 由来=%v)。#185 の判断材料なので記録を残すこと",
				cs.Name, cs.InstallDate, cs.IsHostComputerSystem(), cs.installDateSuggestsHost())
		}
	}
}

// TestPickHostComputerSystem_ErrorIncludesSignalHint は、Name 規則が崩れたときの
// エラーに第 2 の材料の内訳が入ることを検証する (#185)。
//
// 「ホストが見つからない」だけでは Name 規則が壊れたのか応答がおかしいのか分からない。
func TestPickHostComputerSystem_ErrorIncludesSignalHint(t *testing.T) {
	const guid = "A1B2C3D4-1111-2222-3333-444455556666"

	// ホスト不在: 全部 GUID。InstallDate 側は 1 件をホストと見る (= Name 規則が崩れている)。
	noHost := []*Msvm_ComputerSystem{
		{Name: guid, InstallDate: ""},
		{Name: "B2C3D4E5-1111-2222-3333-444455556667", InstallDate: "2026-10-08T15:15:36Z"},
	}
	_, err := pickHostComputerSystem(noHost)
	if err == nil {
		t.Fatal("ホスト不在なのでエラーになるはず")
	}
	for _, want := range []string{"全 2 件", "InstallDate が空なのは 1 件"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラーに %q が無い: %v", want, err)
		}
	}

	// 候補が複数: GUID でない名前が 2 件。
	twoHosts := []*Msvm_ComputerSystem{
		{Name: "WIN-HYPERVHOST", InstallDate: ""},
		{Name: "WIN-OTHERHOST", InstallDate: ""},
		{Name: guid, InstallDate: "2026-10-08T15:15:36Z"},
	}
	_, err = pickHostComputerSystem(twoHosts)
	if err == nil {
		t.Fatal("候補が複数なのでエラーになるはず")
	}
	for _, want := range []string{"2 件あり一意に決まらない", "全 3 件", "InstallDate が空なのは 2 件"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("エラーに %q が無い: %v", want, err)
		}
	}
}
