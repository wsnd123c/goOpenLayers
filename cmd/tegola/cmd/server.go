package cmd

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/go-spatial/cobra"
	"github.com/go-spatial/tegola/atlas"
	"github.com/go-spatial/tegola/cmd/internal/register"
	"github.com/go-spatial/tegola/config/source"
	"github.com/go-spatial/tegola/dict"
	"github.com/go-spatial/tegola/internal/build"
	gdcmd "github.com/go-spatial/tegola/internal/cmd"
	"github.com/go-spatial/tegola/internal/log"
	"github.com/go-spatial/tegola/observability"
	"github.com/go-spatial/tegola/provider"
	"github.com/go-spatial/tegola/server"
)

var (
	serverPort      string
	serverNoCache   bool
	defaultHTTPPort = ":8080"
)

var serverCmd = &cobra.Command{
	Use:     "serve",
	Short:   "Use tegola as a tile server",
	Aliases: []string{"server"},
	Long:    `Use tegola as a vector tile server. Maps tiles will be served at /maps/:map_name/:z/:x/:y`,
	Run: func(cmd *cobra.Command, args []string) {
		gdcmd.New()
		gdcmd.OnComplete(provider.Cleanup)
		gdcmd.OnComplete(observability.Cleanup)

		// check config for server port setting
		// if you set the port via the command line it will override the port setting in the config
		if serverPort == defaultHTTPPort && conf.Webserver.Port != "" {
			serverPort = string(conf.Webserver.Port)
		}

		if conf.Webserver.HostName.Host != "" {
			u := url.URL(conf.Webserver.HostName)
			server.HostName = &u
		}

		// set our server version
		server.Version = build.Version
		build.Commands = append(build.Commands, cmd.Name())
		atlas.StartSubProcesses()

		// set user defined response headers
		for name, value := range conf.Webserver.Headers {
			// cast to string
			val := fmt.Sprintf("%v", value)
			// check that we have a value set
			if val == "" {
				log.Errorf("webserver.header (%v) has no configured value", val)
				os.Exit(1)
			}

			server.Headers[name] = val
		}

		if conf.Webserver.URIPrefix != "" {
			server.URIPrefix = string(conf.Webserver.URIPrefix)
		}

		if conf.Webserver.ProxyProtocol != "" {
			server.ProxyProtocol = string(conf.Webserver.ProxyProtocol)
		}

		if conf.Webserver.SSLCert+conf.Webserver.SSLKey != "" {
			if conf.Webserver.SSLCert == "" {
				// error
				log.Error("config must have both or nether ssl_key and ssl_cert, missing ssl_cert")
				os.Exit(1)
			}

			if conf.Webserver.SSLKey == "" {
				// error
				log.Error("config must have both or nether ssl_key and ssl_cert, missing ssl_key")
				os.Exit(1)
			}

			server.SSLCert = string(conf.Webserver.SSLCert)
			server.SSLKey = string(conf.Webserver.SSLKey)
		}

		// initialize config source if configured
		var configWatcher *source.ConfigWatcher
		log.Infof("Full config struct: %+v", conf)
		log.Infof("Checking app config source: %+v", conf.AppConfigSource)
		log.Infof("AppConfigSource length: %d", len(conf.AppConfigSource))
		if len(conf.AppConfigSource) > 0 {
			log.Info("Initializing app config source...")
			configWatcher = initConfigSource(context.Background())
		} else {
			log.Info("No app config source configured")
		}

		// 将配置传递给 myhttp 模块
		server.SetGlobalConfig(&conf)

		// start our webserver
		srv := server.Start(nil, serverPort)
		shutdown(srv)

		// cleanup config watcher
		if configWatcher != nil {
			gdcmd.OnComplete(func() {
				configWatcher.Close()
			})
		}

		<-gdcmd.Cancelled()
		gdcmd.Complete()
	},
}

func initConfigSource(ctx context.Context) *source.ConfigWatcher {
	// get config source type
	sourceType, err := conf.AppConfigSource.String("type", nil)
	if err != nil {
		log.Errorf("Failed to get config source type: %v", err)
		return nil
	}

	// get base directory for relative paths
	baseDir := filepath.Dir(conf.LocationName)

	// initialize config source
	configSource, err := source.InitSource(sourceType, conf.AppConfigSource, baseDir)
	if err != nil {
		log.Errorf("Failed to initialize config source: %v", err)
		return nil
	}

	// start watching for config changes
	watcher, err := configSource.LoadAndWatch(ctx)
	if err != nil {
		log.Errorf("Failed to start config watcher: %v", err)
		return nil
	}

	// process config updates in a goroutine
	go func() {
		for {
			select {
			case app := <-watcher.Updates:
				log.Infof("Received config update for app: %s", app.Key)
				handleConfigUpdate(app)

			case deletedKey := <-watcher.Deletions:
				log.Infof("Received config deletion for app: %s", deletedKey)
				handleConfigDeletion(deletedKey)

			case <-ctx.Done():
				log.Info("Config watcher context cancelled")
				return
			}
		}
	}()

	return &watcher
}

func handleConfigUpdate(app source.App) {

	//这是个map的打印
	// convert providers to dict.Dicter format
	provArr := make([]dict.Dicter, len(app.Providers))
	for i := range provArr {
		provArr[i] = app.Providers[i]
		fmt.Println(provArr[i])
	}

	// register new providers
	providers, err := register.Providers(provArr, app.Maps, nil, nil)
	fmt.Printf("这是provArr:%+v\n", provArr)
	if err != nil {
		log.Errorf("Failed to register providers for app %s: %v", app.Key, err)
		return
	}
	for i, p := range app.Providers {
		fmt.Printf("Provider[%d] = %#v\n", i, p)
	}

	// register new maps
	if err = register.Maps(nil, app.Maps, providers); err != nil {
		log.Errorf("Failed to register maps for app %s: %v", app.Key, err)
		return
	}

	log.Infof("Successfully updated configuration for app: %s", app.Key)
}

func handleConfigDeletion(key string) {
	// TODO: Implement config deletion logic
	// This would involve unregistering providers and maps associated with the key
	log.Infof("Config deletion not yet implemented for key: %s", key)
}

func shutdown(srv *http.Server) {
	gdcmd.OnComplete(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel() // releases resources if slowOperation completes before timeout elapses
		srv.Shutdown(ctx)
	})
}
