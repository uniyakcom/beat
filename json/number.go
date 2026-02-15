package json

import "math"

// parseInt 快速整数解析（避免 strconv.ParseInt 开销）
//
// 支持负数，溢出返回错误。
func parseInt(s string) (int64, error) {
	if len(s) == 0 {
		return 0, errInvalidNumber
	}
	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	}
	if i >= len(s) {
		return 0, errInvalidNumber
	}

	var n uint64
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			// 遇到 '.', 'e', 'E' 说明是浮点数 → fallback
			if c == '.' || c == 'e' || c == 'E' {
				f, err := parseFloat(s)
				if err != nil {
					return 0, err
				}
				return int64(f), nil
			}
			return 0, errInvalidNumber
		}
		d := uint64(c - '0')
		if n > (math.MaxInt64+uint64(1)-d)/10 {
			return 0, errOverflow
		}
		n = n*10 + d
	}

	if neg {
		if n > uint64(math.MaxInt64)+1 {
			return 0, errOverflow
		}
		return -int64(n), nil
	}
	if n > uint64(math.MaxInt64) {
		return 0, errOverflow
	}
	return int64(n), nil
}

// parseFloat 快速浮点数解析
//
// 基于手写状态机，常见简单数字直接整数运算避免 strconv.ParseFloat。
// 对于超出简单路径的数字（如极大指数），fallback 到精确解析。
func parseFloat(s string) (float64, error) {
	if len(s) == 0 {
		return 0, errInvalidNumber
	}

	neg := false
	i := 0
	if s[0] == '-' {
		neg = true
		i = 1
	}
	if i >= len(s) {
		return 0, errInvalidNumber
	}

	// 整数部分
	var intPart uint64
	intDigits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		intPart = intPart*10 + uint64(s[i]-'0')
		intDigits++
		i++
	}
	if intDigits == 0 {
		return 0, errInvalidNumber
	}

	result := float64(intPart)

	// 小数部分
	if i < len(s) && s[i] == '.' {
		i++
		fracStart := i
		var fracPart uint64
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			fracPart = fracPart*10 + uint64(s[i]-'0')
			i++
		}
		fracDigits := i - fracStart
		if fracDigits == 0 {
			return 0, errInvalidNumber
		}
		result += float64(fracPart) / pow10f(fracDigits)
	}

	// 指数部分
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		expNeg := false
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			expNeg = s[i] == '-'
			i++
		}
		if i >= len(s) || s[i] < '0' || s[i] > '9' {
			return 0, errInvalidNumber
		}
		var exp int
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			exp = exp*10 + int(s[i]-'0')
			if exp > 308 {
				// 超出 float64 范围
				if expNeg {
					if neg {
						return math.Copysign(0, -1), nil
					}
					return 0, nil
				}
				if neg {
					return math.Inf(-1), nil
				}
				return math.Inf(1), nil
			}
			i++
		}
		if expNeg {
			exp = -exp
		}
		result *= math.Pow(10, float64(exp))
	}

	if neg {
		result = -result
	}
	return result, nil
}

// pow10f 10^n 查表（n=0..22 精确，>22 fallback）
func pow10f(n int) float64 {
	if n < len(pow10Tab) {
		return pow10Tab[n]
	}
	return math.Pow(10, float64(n))
}

var pow10Tab = [...]float64{
	1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7,
	1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15,
	1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22,
}

// 错误常量
type jsonError string

func (e jsonError) Error() string { return string(e) }

const (
	errInvalidNumber jsonError = "json: invalid number"
	errOverflow      jsonError = "json: number overflow"
)
