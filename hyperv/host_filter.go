package hyperv

import (
	"fmt"

	"github.com/google/uuid"
)

// IsHostComputerSystem は この Msvm_ComputerSystem が Hyper-V ホスト自身 (管理 OS) かを返す。
//
// Msvm_ComputerSystem は VM とホストの両方を表すため、VM 一覧を取るときは除外が要る (#139)。
//
// 判定に Name を使う理由: MOF は Name を "always set to GUID" と書いているが、**実機のホスト
// エントリは GUID ではなくコンピューター名を返す** (2026-09-27 実測: ホスト Name="DESKTOP-..."、
// VM Name="A1B2C3D4-1111-2222-3333-444455556666")。VM 側は必ず GUID なので、GUID として
// 解釈できるかどうかで切れる。以降のコメント中のホスト名・GUID は匿名化した例示値で、
// 実機の値そのものではない (本リポジトリは公開のため)。
//
// Caption / Description を使わないのは、これらが**ロケールで変わる**ため (クラスに AMENDMENT
// 修飾子が付く)。実機 (日本語ホスト) では Caption がホスト「ホスト コンピューター システム」/
// VM「仮想マシン」、Description がホスト「Microsoft ホスト コンピューター システム」/
// VM "Microsoft Virtual Machine" と返る。**Description はホストだけ日本語で VM は英語**という
// 非対称があり、VM だけ見て検証すると「英語だから安全」と誤判断する。#98 と同型の罠。
//
// もう 1 つのロケール非依存な材料として InstallDate がある (MOF: VM は構成の作成日時、
// 管理 OS は Null。実機でもホストだけ xsi:nil)。
//
// **実機の記録が入ったので AND 条件に締められる状態になった** (#185 で追跡)。
// recorded_computersystem_pull_2.xml (ホスト) は InstallDate が xsi:nil、
// pull_3/4/5 (VM) は値を持つ。まだ Name 単独で判定しているのは、締めると
// 「InstallDate を返さない実機」で全 VM がホスト扱いになる破壊的な失敗をしうるため
// (実機 1 台の観測で AND に締めるのは早い)。
func (cs *Msvm_ComputerSystem) IsHostComputerSystem() bool {
	_, err := uuid.Parse(cs.Name)
	return err != nil
}

// pickHostComputerSystem は列挙結果からホスト自身を 1 件だけ取り出す。
//
// 不在・複数一致はエラーにする。IsHostComputerSystem の判定 (Name が GUID として
// 解釈できないか) が将来崩れたときに、黙って別のインスタンスを掴むより落ちて気付く方を選ぶ。
//
// Client から切り出してあるのは、この分岐を struct だけでテストできるようにするため。
// 配線は実機記録で固定済 (#167、host_exclusion_wiring_test.go)。
func pickHostComputerSystem(all []*Msvm_ComputerSystem) (*Msvm_ComputerSystem, error) {
	hosts := filterHostComputerSystems(all)
	switch len(hosts) {
	case 1:
		return hosts[0], nil
	case 0:
		return nil, fmt.Errorf("pickHostComputerSystem: ホストの Msvm_ComputerSystem が見つからない")
	default:
		return nil, fmt.Errorf("pickHostComputerSystem: ホスト候補が %d 件あり一意に決まらない", len(hosts))
	}
}

// filterHostComputerSystems は列挙結果から Hyper-V ホスト自身だけを残す。
func filterHostComputerSystems(all []*Msvm_ComputerSystem) []*Msvm_ComputerSystem {
	out := make([]*Msvm_ComputerSystem, 0, 1)
	for _, cs := range all {
		if cs.IsHostComputerSystem() {
			out = append(out, cs)
		}
	}
	return out
}

// filterOutHostComputerSystems は列挙結果から Hyper-V ホスト自身を取り除く。
//
// ListComputerSystems から切り出してあるのは、この絞り込みを struct だけで
// テストできるようにするため。
//
// **配線 (ListComputerSystems が実際に呼んでいるか) は実機記録で固定済** (#167)。
// host_exclusion_wiring_test.go を参照。
func filterOutHostComputerSystems(all []*Msvm_ComputerSystem) []*Msvm_ComputerSystem {
	out := make([]*Msvm_ComputerSystem, 0, len(all))
	for _, cs := range all {
		if cs.IsHostComputerSystem() {
			continue
		}
		out = append(out, cs)
	}
	return out
}
