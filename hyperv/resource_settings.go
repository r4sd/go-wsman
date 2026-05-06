package hyperv

import (
	"context"
	"fmt"

	"github.com/r4sd/go-wsman/wsman"
)

// ResourceSettingsResult は AddResourceSettings / ModifyResourceSettings の結果を表す。
//
// ResultingResourceSettings は実際には配列だが、現行の Invoke レスポンスパーサ
// (extractProperties) が同名要素を上書き保存するため最後の 1 件しか取得できない。
// 単一リソース追加/変更が大半のユースケースなので Phase 4 part 1 では許容する。
type ResourceSettingsResult struct {
	JobRef                    string // 非同期 Job 参照
	ResultingResourceSettings string // 追加/変更されたリソースの EPR (最後の 1 件)
	ReturnValue               string // "0"=同期成功, "4096"=非同期 Job 開始
}

// AddResourceSettings は VM に新しいリソース (NIC/Disk/DVD/Memory 拡張等) を追加する。
//
// vmName: 対象 VM の Msvm_ComputerSystem.Name (GUID)
// settingsXML: 追加する SettingData の Embedded Instance XML スライス
//
// 内部で対象 VM の Msvm_VirtualSystemSettingData (Realized) を取得し、
// その InstanceID で EPR を組み立てて AffectedConfiguration として送信する。
//
// CIM 仕様:
//
//	Msvm_VirtualSystemManagementService.AddResourceSettings(
//	  [in] CIM_VirtualSystemSettingData REF AffectedConfiguration,
//	  [in] string ResourceSettings[],
//	  [out] CIM_ResourceAllocationSettingData REF ResultingResourceSettings[],
//	  [out] CIM_ConcreteJob REF Job
//	)
func (c *Client) AddResourceSettings(ctx context.Context, vmName string, settingsXML []string) (*ResourceSettingsResult, error) {
	if vmName == "" {
		return nil, fmt.Errorf("AddResourceSettings: vmName must not be empty")
	}
	if len(settingsXML) == 0 {
		return nil, fmt.Errorf("AddResourceSettings: settingsXML must not be empty")
	}

	settings, err := c.GetSystemSettingData(ctx, vmName)
	if err != nil {
		return nil, fmt.Errorf("AddResourceSettings: get setting data: %w", err)
	}

	epr := buildEndpointReference(msvmVirtualSystemSettingDataURI, map[string]string{
		"InstanceID": settings.InstanceID,
	})

	params := make([]wsman.Param, 0, 1+len(settingsXML))
	params = append(params, wsman.Param{Name: "AffectedConfiguration", Value: epr})
	for _, s := range settingsXML {
		params = append(params, wsman.Param{Name: "ResourceSettings", Value: s})
	}

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualSystemManagementServiceURI, "AddResourceSettings", params)
	if err != nil {
		return nil, err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return nil, fmt.Errorf("AddResourceSettings: unexpected ReturnValue=%s", rv)
	}
	result := &ResourceSettingsResult{
		JobRef:                    resp.Property("Job"),
		ResultingResourceSettings: resp.Property("ResultingResourceSettings"),
		ReturnValue:               rv,
	}
	if rv == "4096" && result.JobRef == "" {
		return nil, fmt.Errorf("AddResourceSettings: ReturnValue=4096 but no Job reference")
	}
	return result, nil
}

// ModifyResourceSettings は既存リソースの設定を変更する。
//
// settingsXML 各要素は InstanceID を持ち、それで対象を特定する。
// Memory/CPU の Modify では VM 作成時のデフォルト設定を取得して書き換える流れ。
//
// CIM 仕様:
//
//	Msvm_VirtualSystemManagementService.ModifyResourceSettings(
//	  [in] string ResourceSettings[],
//	  [out] CIM_ResourceAllocationSettingData REF ResultingResourceSettings[],
//	  [out] CIM_ConcreteJob REF Job
//	)
func (c *Client) ModifyResourceSettings(ctx context.Context, settingsXML []string) (*ResourceSettingsResult, error) {
	if len(settingsXML) == 0 {
		return nil, fmt.Errorf("ModifyResourceSettings: settingsXML must not be empty")
	}

	params := make([]wsman.Param, 0, len(settingsXML))
	for _, s := range settingsXML {
		params = append(params, wsman.Param{Name: "ResourceSettings", Value: s})
	}

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualSystemManagementServiceURI, "ModifyResourceSettings", params)
	if err != nil {
		return nil, err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return nil, fmt.Errorf("ModifyResourceSettings: unexpected ReturnValue=%s", rv)
	}
	result := &ResourceSettingsResult{
		JobRef:                    resp.Property("Job"),
		ResultingResourceSettings: resp.Property("ResultingResourceSettings"),
		ReturnValue:               rv,
	}
	if rv == "4096" && result.JobRef == "" {
		return nil, fmt.Errorf("ModifyResourceSettings: ReturnValue=4096 but no Job reference")
	}
	return result, nil
}

// RemoveResourceSettings は既存リソースを削除する。
//
// resourceRefs: 削除対象リソースの EPR スライス。
//
// CIM 仕様:
//
//	Msvm_VirtualSystemManagementService.RemoveResourceSettings(
//	  [in] CIM_ResourceAllocationSettingData REF ResourceSettings[],
//	  [out] CIM_ConcreteJob REF Job
//	)
func (c *Client) RemoveResourceSettings(ctx context.Context, resourceRefs []string) (string, error) {
	if len(resourceRefs) == 0 {
		return "", fmt.Errorf("RemoveResourceSettings: resourceRefs must not be empty")
	}

	params := make([]wsman.Param, 0, len(resourceRefs))
	for _, r := range resourceRefs {
		params = append(params, wsman.Param{Name: "ResourceSettings", Value: r})
	}

	resp, err := c.wsman.InvokeMulti(ctx, msvmVirtualSystemManagementServiceURI, "RemoveResourceSettings", params)
	if err != nil {
		return "", err
	}

	rv := resp.ReturnValue
	if rv != "0" && rv != "4096" {
		return "", fmt.Errorf("RemoveResourceSettings: unexpected ReturnValue=%s", rv)
	}
	jobRef := resp.Property("Job")
	if rv == "4096" && jobRef == "" {
		return "", fmt.Errorf("RemoveResourceSettings: ReturnValue=4096 but no Job reference")
	}
	return jobRef, nil
}
