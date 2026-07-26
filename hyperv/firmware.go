package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const msvmBootSourceSettingDataURI = nsVirtV2 + "/Msvm_BootSourceSettingData"

// ListBootSources は指定 VM のファームウェアブートソース (Msvm_BootSourceSettingData) を列挙する。
//
// Gen2 VM の Msvm_VirtualSystemSettingData.BootSourceOrder[] が参照する先。BootSourceOrder[] の
// 各要素は WMI オブジェクトパス文字列 (Parent/HostResource と同形式) で、この InstanceID を指す。
// 順序は BootSourceOrder[] 側の並びで決まり、本メソッドの戻り値の並び順に意味はない。パス文字列
// から InstanceID を取り出す処理は go-wsman にはまだ無く (未実装、Slice D 本実装で追加予定)、
// 呼び出し側で BootSourceOrder と突き合わせる。
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

// BootSourceRef は BootSourceOrder[] に書き込む WMI オブジェクトパス参照文字列を生成する。
//
// deviceInstanceID (NIC/Drive の Msvm_SyntheticEthernetPortSettingData や
// Msvm_ResourceAllocationSettingData の InstanceID) に "\B" を付けたものが対象
// Msvm_BootSourceSettingData.InstanceID と一致する実機確認済みの対応規則 (ListBootSources 側で
// 読み取り時に検証済み、resolveBootOrders 参照)。この関数はその逆変換で、
// Msvm_VirtualSystemSettingData.BootSourceOrder[] に書き込む参照文字列を組み立てる。
func (c *Client) BootSourceRef(deviceInstanceID string) string {
	return wmiObjectPath(c.hostName, msvmBootSourceSettingDataURI, map[string]string{
		"InstanceID": deviceInstanceID + `\B`,
	})
}
