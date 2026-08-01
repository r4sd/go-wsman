package hyperv

import (
	"strings"
	"testing"
)

// TestMatchSettingDataVM は SettingData の InstanceID から VM を絞り込む述語を検証する。
//
// 元の WQL `InstanceID LIKE 'Microsoft:<guid>%'` を Go 側に移したもの。
// InstanceID は "Microsoft:<VM_GUID>\<RES_GUID>" 形式で、prefix の前方一致で判定する。
func TestMatchSettingDataVM(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	tests := []struct {
		name       string
		instanceID string
		vmGUID     string
		want       bool
	}{
		{
			name:       "正常: 対象 VM のリソース (Microsoft:<guid>\\<res>)",
			instanceID: `Microsoft:11111111-aaaa-bbbb-cccc-000000000001\F4DA67D7`,
			vmGUID:     vm,
			want:       true,
		},
		{
			name:       "正常: VM 直下 (区切り \\ 無しでも prefix 一致)",
			instanceID: "Microsoft:11111111-aaaa-bbbb-cccc-000000000001",
			vmGUID:     vm,
			want:       true,
		},
		{
			name:       "除外: 別 VM のリソース",
			instanceID: `Microsoft:22222222-aaaa-bbbb-cccc-000000000002\NIC-001`,
			vmGUID:     vm,
			want:       false,
		},
		{
			name:       "除外: prefix (Microsoft:) が無い",
			instanceID: `11111111-aaaa-bbbb-cccc-000000000001\NIC`,
			vmGUID:     vm,
			want:       false,
		},
		{
			name:       "除外: GUID の大小文字違い (LIKE と異なり区別する)",
			instanceID: `Microsoft:11111111-AAAA-BBBB-CCCC-000000000001\NIC`,
			vmGUID:     vm,
			want:       false,
		},
		{
			name:       "除外: GUID が部分的に異なる接頭辞 (前方一致だが GUID 末尾が違う)",
			instanceID: `Microsoft:11111111-aaaa-bbbb-cccc-000000000010\NIC`,
			vmGUID:     vm,
			want:       false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchSettingDataVM(tt.instanceID, tt.vmGUID); got != tt.want {
				t.Errorf("matchSettingDataVM(%q, %q) = %v, want %v", tt.instanceID, tt.vmGUID, got, tt.want)
			}
		})
	}
}

// TestMatchRealizedSettingDataForVM は VirtualSystemSettingData を VM + Realized で
// 絞り込む述語を検証する (元 WQL: VirtualSystemIdentifier="<vm>" AND VirtualSystemType="Realized")。
func TestMatchRealizedSettingDataForVM(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	tests := []struct {
		name         string
		vmIdentifier string
		vmType       string
		vmName       string
		want         bool
	}{
		{
			name:         "正常: 対象 VM の Realized 構成",
			vmIdentifier: vm,
			vmType:       VirtualSystemTypeRealized,
			vmName:       vm,
			want:         true,
		},
		{
			name:         "除外: 対象 VM の Snapshot:Realized (Realized ではない)",
			vmIdentifier: vm,
			vmType:       VirtualSystemTypeSnapshotRealized,
			vmName:       vm,
			want:         false,
		},
		{
			name:         "除外: 別 VM の Realized 構成",
			vmIdentifier: "22222222-aaaa-bbbb-cccc-000000000002",
			vmType:       VirtualSystemTypeRealized,
			vmName:       vm,
			want:         false,
		},
		{
			name:         "除外: Planned 構成",
			vmIdentifier: vm,
			vmType:       VirtualSystemTypePlanned,
			vmName:       vm,
			want:         false,
		},
		{
			// GUID の表記揺れで「エラーなしで 0 件」になる故障を防ぐ (#103 と同じ故障モード)。
			name:         "正常: GUID の大文字小文字が異なっても一致する",
			vmIdentifier: strings.ToUpper(vm),
			vmType:       VirtualSystemTypeRealized,
			vmName:       vm,
			want:         true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchRealizedSettingDataForVM(tt.vmIdentifier, tt.vmType, tt.vmName); got != tt.want {
				t.Errorf("matchRealizedSettingDataForVM(%q, %q, %q) = %v, want %v",
					tt.vmIdentifier, tt.vmType, tt.vmName, got, tt.want)
			}
		})
	}
}

// TestMatchSnapshotSettingDataForVM はチェックポイント絞り込みの述語を検証する。
// VirtualSystemType の値は実機・MOF ともに "Microsoft:Hyper-V:Snapshot:Realized"
// (VM 本体と違い "System:" セグメントを含まない) で、ここを取り違えると
// 「エラーなしで常に 0 件」になる。
func TestMatchSnapshotSettingDataForVM(t *testing.T) {
	const vm = "11111111-aaaa-bbbb-cccc-000000000001"
	tests := []struct {
		name         string
		vmIdentifier string
		vmType       string
		want         bool
	}{
		{"正常: 対象 VM の Snapshot:Realized", vm, VirtualSystemTypeSnapshotRealized, true},
		{"正常: GUID の大文字小文字が異なっても一致する", strings.ToUpper(vm), VirtualSystemTypeSnapshotRealized, true},
		{"除外: 対象 VM の Realized 構成 (本体)", vm, VirtualSystemTypeRealized, false},
		{"除外: 別 VM の Snapshot", "22222222-aaaa-bbbb-cccc-000000000002", VirtualSystemTypeSnapshotRealized, false},
		{"除外: 誤った旧定数値 (System: セグメント入り)", vm, "Microsoft:Hyper-V:System:Snapshot:Realized", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchSnapshotSettingDataForVM(tt.vmIdentifier, tt.vmType, vm); got != tt.want {
				t.Errorf("matchSnapshotSettingDataForVM(%q, %q, %q) = %v, want %v",
					tt.vmIdentifier, tt.vmType, vm, got, tt.want)
			}
		})
	}
}

// TestMatchRealizedSettingData は VirtualSystemType=Realized のみで絞り込む述語を検証する
// (元 WQL: VirtualSystemType="Realized")。
func TestMatchRealizedSettingData(t *testing.T) {
	tests := []struct {
		name   string
		vmType string
		want   bool
	}{
		{name: "正常: Realized", vmType: VirtualSystemTypeRealized, want: true},
		{name: "除外: Snapshot:Realized", vmType: VirtualSystemTypeSnapshotRealized, want: false},
		{name: "除外: Planned", vmType: VirtualSystemTypePlanned, want: false},
		{name: "除外: 空文字", vmType: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchRealizedSettingData(tt.vmType); got != tt.want {
				t.Errorf("matchRealizedSettingData(%q) = %v, want %v", tt.vmType, got, tt.want)
			}
		})
	}
}
