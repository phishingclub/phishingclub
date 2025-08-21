package cli

import (
	"fmt"

	"github.com/fatih/color"
)

// PrintVersion outputs the version of the application
func PrintVersion(
	name,
	version string,
) {
	fmt.Printf("%s (%s)\n", name, version)
}

// PrintBanner outputs the banner for the application
func PrintBanner() {
	blue := color.New(color.FgBlue)
	_, _ = blue.Println(`

                       --:
                     .@@@@@*-.
                     .@@@@@@@@++:
        .+*=.        .@@@@@@@@@@@@*-.
        +@@@@++-     .+@@@@@@@@@@@@@@#=:
        *@@@@@@@@#=.  .=#@@@@@@@@@@@@@@@+*-
        *@@@@@@@@@@@+-    :#@@@@@@@@@@@@@@@@#.
        *@@@@@@@@@@@@=     +@@@@@@@@@@@@@@@@@=
        *@@@@@@@@++:   .=#@@@@@@@@@@@@@@@@++:
        *@@@@@*=.    .+@@@@@@@@@@@@@@@@#=.
        .*#+:        .@@@@@@@@@@@@@+*-
                     .@@@@@@@@@@#=.
                     .@@@@@@+*-
                      ++@#=.                      `)
	_, _ = fmt.Println()
	_, _ = fmt.Println()
}
func PrintServerStarted(
	name string,
	address string,
) {
	fmt.Printf("%s available:\nhttps://%s\n\n", name, address)
}
