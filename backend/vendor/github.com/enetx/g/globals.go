package g

import (
	"os"
)

const (
	ASCII_LETTERS   String = ASCII_LOWERCASE + ASCII_UPPERCASE
	ASCII_LOWERCASE String = "abcdefghijklmnopqrstuvwxyz"
	ASCII_UPPERCASE String = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	DIGITS          String = "0123456789"
	HEXDIGITS       String = "0123456789abcdefABCDEF"
	OCTDIGITS       String = "01234567"
	PUNCTUATION     String = `!"#$%&'()*+,-./:;<=>?@[\]^{|}~` + "`"

	FileDefault os.FileMode = 0o644
	FileCreate  os.FileMode = 0o666
	DirDefault  os.FileMode = 0o755
	FullAccess  os.FileMode = 0o777

	PathSeperator = String(os.PathSeparator)
)
