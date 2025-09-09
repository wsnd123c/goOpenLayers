package source

import (
	"context"
	"fmt"
	"github.com/go-spatial/tegola/internal/env"
	"github.com/go-spatial/tegola/internal/log"
	"github.com/go-spatial/tegola/nacos"
)

// NacosConfigSource is a config source for loading and watching setting in nacos.
type NacosConfigSource struct {
	ip          string
	port        string
	nameSpaceId string
	dataId      string
	group       string
	username    string
	password    string
}

func (s *NacosConfigSource) Init(options env.Dict) error {
	var err error
	ip, err := options.String("ip", nil)
	if err != nil {
		return err
	}

	port, err := options.String("port", nil)
	if err != nil {
		return err
	}

	nameSpaceId, err := options.String("nameSpaceId", nil)
	if err != nil {
		return err
	}

	dataId, err := options.String("dataId", nil)
	if err != nil {
		return err
	}

	group, err := options.String("group", nil)
	if err != nil {
		return err
	}

	// Username and password are optional
	username, _ := options.String("username", nil)
	password, _ := options.String("password", nil)

	s.ip = ip
	s.port = port
	s.nameSpaceId = nameSpaceId
	s.dataId = dataId
	s.group = group
	s.username = username
	s.password = password
	return nil
}

func (s *NacosConfigSource) Type() string {
	return "nacos"
}

// LoadAndWatch will read all the files in the configured directory and then keep watching the directory for changes.
func (s *NacosConfigSource) LoadAndWatch(ctx context.Context) (ConfigWatcher, error) {
	appWatcher := ConfigWatcher{
		Updates:   make(chan App),
		Deletions: make(chan string),
	}

	// First check that nacos config exists and is readable.
	err := nacos.Init(s.ip, s.port, s.nameSpaceId, s.username, s.password)
	if err != nil {
		return appWatcher, fmt.Errorf("failed to initialize nacos client: %s", err)
	}
	content, err := nacos.Client.Get(s.dataId, s.group)
	if err != nil {
		return appWatcher, fmt.Errorf("nacos config not readable: %s", err)
	}
	
	log.Infof("Initial Nacos config loaded from %s-%s: %s", s.dataId, s.group, content)

	go func() {
		// Load initial config
		log.Info("Loading initial Nacos configuration...")
		s.loadApp(content, appWatcher.Updates)

		// Start listening for config changes
		log.Infof("Starting to listen for Nacos config changes on %s-%s", s.dataId, s.group)
		err = nacos.Client.Listen(s.dataId, s.group, func(content string) {
			log.Infof("🔥 NACOS CONFIG CHANGE DETECTED! 🔥")
			log.Infof("Nacos config updated from %s-%s: %s", s.dataId, s.group, content)
			s.loadApp(content, appWatcher.Updates)
		})
		if err != nil {
			log.Errorf("Failed to start Nacos listener: %s", err.Error())
			return
		}
		
		log.Info("✅ Nacos listener started successfully")

		// Keep the goroutine alive
		for {
			select {
			case <-ctx.Done():
				log.Info("Exiting Nacos watcher...")
				appWatcher.Close()
				return
			}
		}
	}()

	return appWatcher, nil
}

// loadApp reads nacos config content and loads the app into the updates channel.
func (s *NacosConfigSource) loadApp(content string, updates chan App) {
	log.Infof("Processing Nacos config content: %s", content)
	
	if app, err := parseAppFromNacos(content, s.nameSpaceId+s.group+s.dataId); err == nil {
		log.Infof("Successfully parsed Nacos config into app: %+v", app)
		updates <- app
	} else {
		log.Errorf("Failed to parse nacos %s-%s-%s: %s", s.nameSpaceId, s.group, s.dataId, err)
	}
}
