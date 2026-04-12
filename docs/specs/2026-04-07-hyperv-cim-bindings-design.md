# Hyper-V CIM クラスバインディング設計書

## 概要

go-wsman の `hyperv` パッケージとして、Hyper-V CIM クラスの型安全なバインディングを実装する。
最終目標は terraform-provider-hyperv の完全代替。

## 背景

- homelab は Windows 11 Hyper-V 上に VM を構築する基盤
- 現行の [taliesins/terraform-provider-hyperv](https://github.com/taliesins/terraform-provider-hyperv) は開発が停滞中
- go-wsman で WS-Man プロトコル層は実装済み。その上に CIM クラスバインディングを載せる

## アーキテクチャ

```
レイヤー 1: go-wsman/wsman      ← WS-Man プロトコル（実装済み）
レイヤー 2: go-wsman/hyperv     ← CIM クラスバインディング（本設計の対象）
レイヤー 3: terraform-provider  ← 別リポジトリ（将来）
```

## スコープ

### 対象プラットフォーム

- Windows 11 Hyper-V
- CIM 名前空間: `root/virtualization/v2`

### 対象リソース（terraform-provider-hyperv 互換）

| terraform リソース | CIM クラス | Phase |
|-------------------|-----------|-------|
| `hyperv_vhd` | `Msvm_ImageManagementService` | 2 |
| `hyperv_machine_instance` | `Msvm_ComputerSystem`, `Msvm_VirtualSystemManagementService`, 各 SettingData | 3-4 |
| `hyperv_network_switch` | `Msvm_VirtualEthernetSwitch`, `Msvm_VirtualEthernetSwitchManagementService` | 5 |

### 将来の拡張（Win11 対応範囲）

| 機能 | CIM クラス | Phase |
|------|-----------|-------|
| チェックポイント | `Msvm_VirtualSystemSnapshotService` | 6 |
| VM エクスポート | `Msvm_VirtualSystemManagementService.ExportSystemDefinition` | 6 |

### 対象外

- Hyper-V レプリカ（Windows Server 専用）
- Windows Server Backup（CIM 外）
- フェールオーバークラスタリング（Windows Server 専用）

## 設計方針

### マッピング方式: reflection + struct tag

`cim:"PropertyName"` タグで `map[string]string` → Go 構造体へマッピングする。

**選定理由**:
- terraform-provider 代替を目指すと CIM クラスは数十に及ぶ。手書きマッピングはスケールしない
- 実機（homelab）から golden file をダンプして検証できるため、reflection でも型安全性を担保できる
- コード生成（CIM MOF からの自動生成）は将来の選択肢として残すが、MVP では過剰

### 命名規約

- CIM クラス名をそのまま使用: `Msvm_ComputerSystem`, `Msvm_VirtualSystemSettingData`
- Go の命名慣例（CamelCase）より CIM 仕様との一貫性を優先
- フィールド名も CIM プロパティ名そのまま: `ElementName`, `EnabledState`

### テスト戦略

- **単体テスト**: golden file（実機からダンプした XML）を使用
- **統合テスト**: `//go:build integration` タグで分離、実機 Hyper-V に接続
- **環境変数**: 既存の `WSMAN_ENDPOINT`, `WSMAN_USERNAME`, `WSMAN_PASSWORD` を流用

**統合テスト前提条件**（Phase 1）:
- Hyper-V ホスト上に最低 1 つの VM が存在すること
- Phase 1 は読み取り専用のため、VM の作成・削除は行わない
- VM 作成・削除テストは Phase 3 以降

**golden file 命名規約**（wsman パッケージの慣例に合わせる）:
```
testdata/
├── get_response_computersystem.xml       # Get レスポンス
├── enumerate_response_computersystem.xml # Enumerate レスポンス
└── pull_response_computersystem.xml      # Pull レスポンス
```

## Phase 1: Unmarshal + Msvm_ComputerSystem（Issue #7）

### パッケージ構成

```
hyperv/
├── doc.go                # パッケージ概要
├── types.go              # Msvm_ComputerSystem 構造体 + 定数
├── unmarshal.go          # reflection ベースの Unmarshal
├── unmarshal_test.go     # 型変換の単体テスト
├── client.go             # hyperv.Client（wsman.Client ラップ）
├── client_test.go        # golden file テスト
├── testdata/             # 実機からダンプした XML
└── integration_test.go   # //go:build integration
```

### Unmarshal 関数

```go
// Unmarshal は map[string]string を cim タグ付き構造体にマッピングする。
func Unmarshal(props map[string]string, v interface{}) error
```

**対応型**:
- `string`
- `bool`（"TRUE"/"FALSE"）
- `int`, `int64`
- `uint16`, `uint32`, `uint64`
- CIM DateTime → Phase 1 では `string` で受ける

**動作ルール**:
- `cim` タグなしのフィールドはスキップ
- map にないプロパティ → ゼロ値（エラーにしない）
- 型変換失敗 → fail-fast でエラーを返す（部分的な結果は返さない）
- エラーメッセージにフィールド名とプロパティ名を含める
  - 例: `failed to unmarshal field "EnabledState" (cim:"EnabledState"): strconv.ParseUint: parsing "abc": invalid syntax`

**CIM DateTime の扱い**:
- Phase 1: `string` フィールドでそのまま受ける（`yyyymmddHHMMSS.mmmmmmsUUU` 形式）
- 将来: `time.Time` への変換ヘルパー `ParseCIMDateTime(s string) (time.Time, error)` を追加予定

### Msvm_ComputerSystem 構造体（MVP）

terraform-provider で必要な最小フィールドセット:
- `Name`, `ElementName`: VM 識別
- `EnabledState`: 起動状態確認（Phase 3 の RequestStateChange で必要）
- `HealthState`: ヘルスチェック
- その他のプロパティは後続 Phase で必要に応じて追加

```go
// EnabledState 定数（CIM 仕様）
const (
    EnabledStateUnknown  uint16 = 0
    EnabledStateEnabled  uint16 = 2     // Running
    EnabledStateDisabled uint16 = 3     // Off
    EnabledStatePaused   uint16 = 32768 // Paused
    EnabledStateSaved    uint16 = 32769 // Saved
)

type Msvm_ComputerSystem struct {
    Name              string `cim:"Name"`              // VM GUID
    ElementName       string `cim:"ElementName"`       // VM 表示名
    Caption           string `cim:"Caption"`
    Description       string `cim:"Description"`
    EnabledState      uint16 `cim:"EnabledState"`
    HealthState       uint16 `cim:"HealthState"`       // 5=OK
    InstallDate       string `cim:"InstallDate"`       // CIM DateTime（文字列）
    OnTimeInMilliseconds uint64 `cim:"OnTimeInMilliseconds"`
}
```

### Client API

```go
type Client struct {
    wsman *wsman.Client
}

func NewClient(endpoint string, opts ...wsman.ClientOption) (*Client, error)

// GetComputerSystem は Name（VM GUID）で単一 VM を取得する。
// Selector は Name キーのみで一意に特定する（CIM 仕様上 Msvm_ComputerSystem は Name が Key）。
func (c *Client) GetComputerSystem(ctx context.Context, name string) (*Msvm_ComputerSystem, error)

// ListComputerSystems は全 VM を Enumerate する。
func (c *Client) ListComputerSystems(ctx context.Context) ([]*Msvm_ComputerSystem, error)
```

**エラー型**:
- `*wsman.Fault`: SOAP Fault レスポンス（AccessDenied、DestinationUnreachable 等）
- Unmarshal エラー: 型変換失敗（`fmt.Errorf` でラップ）
- 通信エラー: ネットワーク障害、タイムアウト

```go
// 利用例
vm, err := client.GetComputerSystem(ctx, "vm-guid")
if err != nil {
    if fault, ok := err.(*wsman.Fault); ok {
        // SOAP Fault: fault.Code, fault.Subcode, fault.Reason
    }
    // その他のエラー
}
```

### ResourceURI

```go
const (
    nsVirtV2              = "http://schemas.microsoft.com/wbem/wsman/1/wmi/root/virtualization/v2"
    msvmComputerSystemURI = nsVirtV2 + "/Msvm_ComputerSystem"
)
```

Get と Enumerate は同じ ResourceURI を使用する。Get は `wsman.Selector{Name: "Name", Value: name}` で絞り込む。

## Phase 2〜5: CIM クラス対応表

### Phase 2: VHD 管理

| CIM クラス/メソッド | 用途 |
|---------------------|------|
| `Msvm_ImageManagementService.CreateVirtualHardDisk` | VHDX 作成 |
| `Msvm_ImageManagementService.SetVirtualHardDiskSettingData` | 設定変更 |
| File deletion via WS-Man or direct path | VHDX 削除 |

### Phase 3: VM ライフサイクル

| CIM クラス/メソッド | 用途 |
|---------------------|------|
| `Msvm_VirtualSystemManagementService.DefineSystem` | VM 作成 |
| `Msvm_VirtualSystemManagementService.DestroySystem` | VM 削除 |
| `Msvm_VirtualSystemManagementService.ModifySystemSettings` | VM 設定変更 |
| `Msvm_ComputerSystem.RequestStateChange` | 起動/停止/一時停止 |
| `Msvm_VirtualSystemSettingData` | VM 設定の読み取り |

### Phase 4: VM リソース（NIC / Disk / DVD / CPU / Memory）

| CIM クラス | 用途 |
|-----------|------|
| `Msvm_MemorySettingData` | メモリ設定 |
| `Msvm_ProcessorSettingData` | CPU 設定 |
| `Msvm_SyntheticEthernetPortSettingData` | NIC 設定 |
| `Msvm_StorageAllocationSettingData` | ディスクアタッチ |
| `Msvm_VirtualDVDDiskSettingData` | DVD/ISO マウント |
| `Msvm_VirtualSystemManagementService.AddResourceSettings` | リソース追加 |
| `Msvm_VirtualSystemManagementService.ModifyResourceSettings` | リソース変更 |
| `Msvm_VirtualSystemManagementService.RemoveResourceSettings` | リソース削除 |

### Phase 5: 仮想スイッチ

| CIM クラス/メソッド | 用途 |
|---------------------|------|
| `Msvm_VirtualEthernetSwitch` | スイッチ読み取り |
| `Msvm_VirtualEthernetSwitchManagementService.DefineSystem` | スイッチ作成 |
| `Msvm_VirtualEthernetSwitchManagementService.DestroySystem` | スイッチ削除 |

## フェーズ戦略: インターリーブ方式

各フェーズで CIM バインディングと Terraform リソースをセットで実装し、即 homelab に投入して検証する。

```
Phase 1: hyperv - Unmarshal + Msvm_ComputerSystem (read only)     ← Issue #7
Phase 2: hyperv - VHD 操作 → provider: hyperv_vhd                 ← 最初の入れ替え
Phase 3: hyperv - VM CRUD → provider: hyperv_machine_instance (基本)
Phase 4: hyperv - NIC/Disk/DVD/CPU → provider: machine_instance 拡張
Phase 5: hyperv - Virtual Switch → provider: hyperv_network_switch
Phase 6: (拡張) checkpoint / export
```

## 検証方法

```bash
# 単体テスト
go test -race -v ./hyperv/... -count=1

# 統合テスト（実機接続）
WSMAN_ENDPOINT=https://10.0.0.100:5986/wsman \
WSMAN_USERNAME=terraform \
WSMAN_PASSWORD=yourpassword \
go test -race -tags=integration -v ./hyperv/...
```
