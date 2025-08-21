//go:build production

package build

func init() {
	Flags.Production = true
}
