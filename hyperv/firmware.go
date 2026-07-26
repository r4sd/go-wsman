package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const msvmBootSourceSettingDataURI = nsVirtV2 + "/Msvm_BootSourceSettingData"

// ListBootSources は指定 VM のファームウェアブートソース (Msvm_BootSourceSettingData) を列挙する。
//
// Gen2 VM の Msvm_VirtualSystemSettingData.BootSourceOrder[] が参照する先。順序は
// BootSourceOrder[] 側 (WMI オブジェクトパス文字列、既存の EPR 抽出ロジックで InstanceID化) の
// 並びで決まり、本メソッドの戻り値の並び順に意味はない (呼び出し側で BootSourceOrder と突き合わせる)。
func (c *Client) ListBootSources(ctx context.Context, vmGUID string) ([]*Msvm_BootSourceSettingData, error) {
	if vmGUID == "" {
		return nil, fmt.Errorf("ListBootSources: vmGUID must not be empty")
	}
	// Hyper-V は WQL フィルタ列挙を拒否するため無フィルタ列挙 + Go 側フィルタ (#80 の教訓と同じ)。
	instances, err := c.enumerateFiltered(ctx, msvmBootSourceSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmGUID)
	})
	if err != nil {
		return nil, err
	}
	result := make([]*Msvm_BootSourceSettingData, 0, len(instances))
	for _, inst := range instances {
		var b Msvm_BootSourceSettingData
		if err := Unmarshal(inst.Properties(), &b); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_BootSourceSettingData: %w", err)
		}
		result = append(result, &b)
	}
	return result, nil
}
