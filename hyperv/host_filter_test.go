package hyperv

import "testing"

// TestIsHostComputerSystem は Msvm_ComputerSystem がホスト(管理 OS)か VM かの判定を検証する。
//
// 期待値の出どころは実機 (2026-09-27、日本語ロケールの Windows ホスト) の Enumerate 応答。
// 値は匿名化してある (公開リポジトリのため)。形だけが意味を持つ:
//
//	ホスト: Name="WIN-HYPERVHOST"                        InstallDate=xsi:nil
//	VM:     Name="A1B2C3D4-1111-2222-3333-444455556666"   InstallDate=<cim:Datetime>...
func TestIsHostComputerSystem(t *testing.T) {
	tests := []struct {
		name string
		cs   Msvm_ComputerSystem
		want bool
	}{
		{
			// 実機のホストエントリ。Name が GUID ではなくコンピューター名で返る。
			name: "ホスト: Name がコンピューター名",
			cs:   Msvm_ComputerSystem{Name: "WIN-HYPERVHOST", ElementName: "WIN-HYPERVHOST"},
			want: true,
		},
		{
			name: "VM: Name が GUID (大文字)",
			cs:   Msvm_ComputerSystem{Name: "A1B2C3D4-1111-2222-3333-444455556666", ElementName: "vm-worker-01"},
			want: false,
		},
		{
			name: "VM: Name が GUID (小文字)",
			cs:   Msvm_ComputerSystem{Name: "11111111-aaaa-bbbb-cccc-000000000001", ElementName: "vm-1"},
			want: false,
		},
		{
			// ElementName に何が入っていても Name だけで判断する。
			// 「VM の表示名がホスト名と同じ」構成で誤判定しないため。
			name: "VM: 表示名がホスト名と同じでも Name が GUID なら VM",
			cs:   Msvm_ComputerSystem{Name: "B2C3D4E5-1111-2222-3333-444455556667", ElementName: "WIN-HYPERVHOST"},
			want: false,
		},
		{
			name: "空: Name が無ければホスト扱い (VM は必ず GUID を持つ)",
			cs:   Msvm_ComputerSystem{},
			want: true,
		},
		{
			// 「長さ 36」で判定する実装を落とすためのケース。
			name: "GUID もどき: 長さは 36 だが hex ではない",
			cs:   Msvm_ComputerSystem{Name: "ZZZZZZZZ-1111-2222-3333-444455556666"},
			want: true,
		},
		{
			name: "GUID もどき: 桁数が足りない",
			cs:   Msvm_ComputerSystem{Name: "A1B2C3D4-1111-2222-3333-44445555666"},
			want: true,
		},
		{
			// 🔴 **判定が Name 単独であることを固定する (#185)。**
			// InstallDate との AND に変えるとこのケースが VM になる。
			// AND は実機 1 台の観測で挙動を変えることになるので採っていない。
			// 変えるなら #185 の判断材料 (食い違う実機の観測) を揃えてから。
			name: "ホスト: InstallDate に値があっても Name だけで判定する (AND にしていない)",
			cs: Msvm_ComputerSystem{
				Name:        "WIN-HYPERVHOST",
				InstallDate: "2026-10-08T15:15:36.68424Z",
			},
			want: true,
		},
		{
			// OR に変えるとこのケースがホストになり、**VM が一覧から消える**
			// (provider が state を落として orphan / 重複作成に向かう破壊経路)。
			name: "VM: InstallDate が空でも Name が GUID なら VM (OR にしていない)",
			cs:   Msvm_ComputerSystem{Name: "C3D4E5F6-1111-2222-3333-444455556668", InstallDate: ""},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cs.IsHostComputerSystem(); got != tt.want {
				t.Errorf("IsHostComputerSystem() = %v, want %v (Name=%q)", got, tt.want, tt.cs.Name)
			}
		})
	}
}

// TestIsHostComputerSystem_LocaleIndependent は「ロケール依存の文字列で判定していない」ことを固定する。
//
// 実機 (日本語ホスト) では Caption / Description が次のように返る。特に Description は
// **ホストだけ日本語で VM は英語**という非対称があり、VM だけ見ると英語判定が通ってしまう。
//
//	ホスト Caption="ホスト コンピューター システム" Description="Microsoft ホスト コンピューター システム"
//	VM     Caption="仮想マシン"                     Description="Microsoft Virtual Machine"
//
// この 2 つに何を入れても判定が動かないことを確認する (#98 と同型のロケール事故の予防)。
func TestIsHostComputerSystem_LocaleIndependent(t *testing.T) {
	vmName := "A1B2C3D4-1111-2222-3333-444455556666"
	for _, loc := range []struct{ caption, description string }{
		{"仮想マシン", "Microsoft Virtual Machine"},
		{"ホスト コンピューター システム", "Microsoft ホスト コンピューター システム"},
		{"Virtual Machine", "Microsoft Virtual Computer System"},
		{"", ""},
	} {
		host := Msvm_ComputerSystem{Name: "WIN-HYPERVHOST", Caption: loc.caption, Description: loc.description}
		if !host.IsHostComputerSystem() {
			t.Errorf("Caption=%q Description=%q でホスト判定が崩れた", loc.caption, loc.description)
		}
		vm := Msvm_ComputerSystem{Name: vmName, Caption: loc.caption, Description: loc.description}
		if vm.IsHostComputerSystem() {
			t.Errorf("Caption=%q Description=%q で VM をホストと誤判定した", loc.caption, loc.description)
		}
	}
}

// TestFilterOutHostComputerSystems は列挙結果からホストが落ちることを固定する。
//
// ⚠️ これは「落とす条件」のテストで、**ListComputerSystems に実際に配線されているか**は
// 固定できていない。配線を消しても本テストは通る。実機応答を記録した golden が
// 入るまでの暫定 (別 Issue で追跡)。
func TestFilterOutHostComputerSystems(t *testing.T) {
	in := []*Msvm_ComputerSystem{
		{Name: "WIN-HYPERVHOST", ElementName: "WIN-HYPERVHOST"},
		{Name: "A1B2C3D4-1111-2222-3333-444455556666", ElementName: "vm-worker-01"},
		{Name: "B2C3D4E5-1111-2222-3333-444455556667", ElementName: "vm-worker-02"},
	}
	got := filterOutHostComputerSystems(in)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (ホストだけ落ちる)", len(got))
	}
	for _, cs := range got {
		if cs.IsHostComputerSystem() {
			t.Errorf("ホストが残っている: %q", cs.Name)
		}
	}
	// 順序を保つ (呼び出し側が index で参照する経路があるため)。
	if got[0].ElementName != "vm-worker-01" || got[1].ElementName != "vm-worker-02" {
		t.Errorf("順序が変わった: %q, %q", got[0].ElementName, got[1].ElementName)
	}
}

// TestPickHostComputerSystem はホスト 1 件の取り出しと、不在・複数一致のエラーを検証する。
//
// Internal スイッチの HostResource にはホスト側 Msvm_ComputerSystem のキーが要る (#178)。
// 誤ったインスタンスを掴むと、作られたスイッチが別の宛先に繋がる (静かな誤接続) ので、
// 一意に決まらない場合は落とす。
//
// **ホストを先頭以外に置く。** 先頭に置くと「先頭 1 件を返す」だけの実装でも通ってしまう。
func TestPickHostComputerSystem(t *testing.T) {
	t.Run("ホスト 1 件", func(t *testing.T) {
		got, err := pickHostComputerSystem([]*Msvm_ComputerSystem{
			{Name: "A1B2C3D4-1111-2222-3333-444455556666", ElementName: "vm-worker-01"},
			{Name: "B2C3D4E5-1111-2222-3333-444455556667", ElementName: "vm-worker-02"},
			{Name: "WIN-HYPERVHOST", ElementName: "WIN-HYPERVHOST"},
		})
		if err != nil {
			t.Fatalf("pickHostComputerSystem: %v", err)
		}
		if got.Name != "WIN-HYPERVHOST" {
			t.Errorf("ホストではないインスタンスを掴んだ: %q", got.Name)
		}
	})

	t.Run("ホスト不在はエラー", func(t *testing.T) {
		_, err := pickHostComputerSystem([]*Msvm_ComputerSystem{
			{Name: "A1B2C3D4-1111-2222-3333-444455556666", ElementName: "vm-worker-01"},
		})
		if err == nil {
			t.Error("ホストが無いのにエラーにならない")
		}
	})

	t.Run("ホスト複数はエラー", func(t *testing.T) {
		// IsHostComputerSystem の判定 (Name が GUID でない) が将来崩れたときに、
		// 黙って 1 件目を使うのではなく落ちること。
		_, err := pickHostComputerSystem([]*Msvm_ComputerSystem{
			{Name: "A1B2C3D4-1111-2222-3333-444455556666", ElementName: "vm-worker-01"},
			{Name: "WIN-HYPERVHOST", ElementName: "WIN-HYPERVHOST"},
			{Name: "WIN-OTHERHOST", ElementName: "WIN-OTHERHOST"},
		})
		if err == nil {
			t.Error("ホスト候補が 2 件あるのにエラーにならない")
		}
	})

	t.Run("空はエラー", func(t *testing.T) {
		if _, err := pickHostComputerSystem(nil); err == nil {
			t.Error("空の列挙でエラーにならない")
		}
	})
}
