package cli

import (
	"github.com/fatih/color"
)

type Outputter interface {
	PrintInitialAdminAccount(username, password string)
}

type cliOutputter struct {
	color *color.Color
}

// NewCLIOutputter creates a new CLIOutputter
func NewCLIOutputter() Outputter {
	return &cliOutputter{
		color: color.New(),
	}
}

func (c *cliOutputter) PrintInitialAdminAccount(
	username,
	password string,
) {
	bold := color.New(color.Bold)
	italic := color.New(color.Bold)
	_, _ = italic.Println("One time credentials for account setup")
	_, _ = c.color.Println()
	_, _ = c.color.Print("Username: ")
	_, _ = bold.Println(username)
	_, _ = c.color.Printf("Password: ")
	_, _ = bold.Println(password)
	_, _ = bold.Println()
	_, _ = c.color.Println()
	c.color.DisableColor()
}
