package hyperv

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// cimTagOptionDatetime は cim タグで CIM の datetime 型を明示するオプション。
//
//	`cim:"AutomaticCriticalErrorActionTimeout,datetime"`
//
// Go 側は read が返す ISO 8601 duration をそのまま string で保持するため、
// reflect.Kind からは string としか推論できない。書き込み時に TYPE="datetime" を
// 送らないと ModifySystemSettings が ErrorCode=32768 で失敗する (#119)。
const cimTagOptionDatetime = "datetime"

// parseCimTag は cim タグを「CIM プロパティ名」と「datetime 指定か」に分解する。
// 未知のオプションは無視する (将来オプションが増えても古いコードが壊れないように)。
func parseCimTag(tag string) (name string, isDatetime bool) {
	name, opts, _ := strings.Cut(tag, ",")
	for _, opt := range strings.Split(opts, ",") {
		if opt == cimTagOptionDatetime {
			isDatetime = true
		}
	}
	return name, isDatetime
}

// cimIntervalPattern は CIM ネイティブの interval 書式 (DSP0004)。
//
//	ddddddddHHMMSS.mmmmmm:000
var cimIntervalPattern = regexp.MustCompile(`^\d{8}\d{2}\d{2}\d{2}\.\d{6}:000$`)

// iso8601DurationPattern は実機の read が返す書式。intervalISO8601Pattern と同じ形だが、
// provider 側とは別実装なのでここにも置く。
var iso8601DurationPattern = regexp.MustCompile(`^P(\d+)DT(\d+)H(\d+)M(\d+)S$`)

// iso8601ToCIMInterval は ISO 8601 duration を CIM ネイティブ interval へ変換する。
//
// 同じプロパティで read と write の wire format が非対称なため要る (実機確認、#119):
//
//	read  : "P0DT0H45M0S"
//	write : "00000000004500.000000:000"
//
// 既に CIM ネイティブ書式ならそのまま返す (read 由来でない値を代入された場合)。
// どちらでもない文字列はエラー。黙って送ると ErrorCode=32768 になるだけで、
// 呼び出し側からは「成功したのに変わらない」に見える。
func iso8601ToCIMInterval(s string) (string, error) {
	if cimIntervalPattern.MatchString(s) {
		return s, nil
	}
	m := iso8601DurationPattern.FindStringSubmatch(s)
	if m == nil {
		return "", fmt.Errorf("iso8601ToCIMInterval: %q は ISO 8601 duration (PnDTnHnMnS) でも CIM interval でもない", s)
	}
	nums := make([]int64, 4)
	for i := range nums {
		v, err := strconv.ParseInt(m[i+1], 10, 64)
		if err != nil {
			return "", fmt.Errorf("iso8601ToCIMInterval: %q の数値解釈に失敗: %w", s, err)
		}
		nums[i] = v
	}
	days, hours, minutes, seconds := nums[0], nums[1], nums[2], nums[3]

	// 時分秒の繰り上がりを吸収してから桁に収める (read が "P0DT0H90M0S" を返す想定は
	// 無いが、呼び出し側が組み立てた値でも壊れないように)。
	total := days*24*3600 + hours*3600 + minutes*60 + seconds
	days = total / (24 * 3600)
	rem := total % (24 * 3600)
	hours, minutes, seconds = rem/3600, (rem%3600)/60, rem%60

	if days > 99999999 {
		return "", fmt.Errorf("iso8601ToCIMInterval: %q は日数が 8 桁に収まらない", s)
	}
	return fmt.Sprintf("%08d%02d%02d%02d.%06d:000", days, hours, minutes, seconds, 0), nil
}
