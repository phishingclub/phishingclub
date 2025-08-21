package app

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-errors/errors"

	"github.com/phishingclub/phishingclub/config"
	"github.com/phishingclub/phishingclub/errs"
)

// SetupConfig sets up the config
func SetupConfig(
	enviroment string,
	configFilePath string,
) (*config.Config, error) {
	configFolder, configFile := filepath.Split(configFilePath)
	filesystem := os.DirFS(configFolder)
	configDTO, err := config.NewDTOFromFile(filesystem, configFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, errs.Wrap(err)
	}
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Printf(" * No config loaded. Creating default config file at %s\n\n", configFilePath)
		var conf *config.Config
		if enviroment == MODE_DEVELOPMENT {
			conf = config.NewDevDefaultConfig()
		} else {
			conf = config.NewProductionDefaultConfig()
		}
		err = conf.WriteToFile(configFilePath)
		configDTO = conf.ToDTO()
		if err != nil {
			return nil, errs.Wrap(err)
		}
	}
	return config.FromDTO(configDTO)
}
