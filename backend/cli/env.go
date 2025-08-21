package cli

import (
	"fmt"
)

// OutputEnv outputs the available environment variables
// These are used for CI or similar enviroment tests
func OutputEnv() {
	fmt.Println("Available environment variables:")
	fmt.Println("APP_MODE = production, development, integration_test")
	fmt.Println("TEST_DB_LOG_LEVEL = silent, debug, error, warn, info")
	fmt.Println("HTTP_PROXY - sets outgoing http proxy")
	fmt.Println("HTTPS_PROXY - sets outgoing https proxy")
	fmt.Println("NO_PROXY - hosts that should not be proxied")

}
