package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

const msvmGpuPartitionSettingDataURI = nsVirtV2 + "/Msvm_GpuPartitionSettingData"

// ListGpuAdapters は指定 VM に割り当てられた GPU パーティション (Msvm_GpuPartitionSettingData) を返す。
//
// vmGUID: 対象 VM の Msvm_ComputerSystem.Name (GUID)。
//
// GPU パーティション未割当の VM では空スライスを返す (エラーではない)。terraform-provider の
// Read が「GPU なし」を差分なしで扱えるようにするための挙動。
//
// enumerate は VM 割当だけでなくホスト能力定義 (InstanceID="Microsoft:Definition\...") も返すが、
// matchSettingDataVM が VM_GUID 前方一致で能力定義・別 VM を除外する。GpuPartitionSettingData の
// enumerate は GPU 設定のみを含むため、ResourceSubType 併用フィルタは不要 (RASD 共有クラスの
// ListDiskDrives 等とは異なる)。Hyper-V は WQL フィルタ列挙を拒否する (#80) ため無フィルタ列挙 +
// Go 側述語フィルタで絞る。
func (c *Client) ListGpuAdapters(ctx context.Context, vmGUID string) ([]*Msvm_GpuPartitionSettingData, error) {
	if vmGUID == "" {
		return nil, fmt.Errorf("ListGpuAdapters: vmGUID must not be empty")
	}

	instances, err := c.enumerateFiltered(ctx, msvmGpuPartitionSettingDataURI, func(inst *wsman.Instance) bool {
		return matchSettingDataVM(inst.Property("InstanceID"), vmGUID)
	})
	if err != nil {
		return nil, err
	}

	result := make([]*Msvm_GpuPartitionSettingData, 0, len(instances))
	for _, inst := range instances {
		var g Msvm_GpuPartitionSettingData
		if err := Unmarshal(inst.Properties(), &g); err != nil {
			return nil, fmt.Errorf("failed to unmarshal Msvm_GpuPartitionSettingData: %w", err)
		}
		result = append(result, &g)
	}
	return result, nil
}
