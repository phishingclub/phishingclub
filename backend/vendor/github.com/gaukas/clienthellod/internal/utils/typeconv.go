package utils

func Uint16ToUint8(a []uint16) []uint8 {
	b := make([]uint8, 0)
	for _, v := range a {
		b = append(b, uint8(v>>8))
		b = append(b, uint8(v))
	}
	return b
}
