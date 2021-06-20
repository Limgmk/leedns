package main

import (
	"io/ioutil"
	"log"
	"net"
	"net/url"

	"github.com/Limgmk/leedns/dns"
	"github.com/Limgmk/leedns/listener"
	"github.com/Limgmk/leedns/resolver"
	flag "github.com/spf13/pflag"
	"gopkg.in/yaml.v2"
)

type Listener struct {
	ServiceType string `yaml:"type"`
	Addr        string `yaml:"addr"`
	CertFile    string `yaml:"certfile"`
	KeyFile     string `yaml:"keyfile"`
	HttpPath    string `yaml:"http-path"`
}

type Upstream struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight"`
}

type Config struct {
	Listener  []*Listener `yaml:"listener"`
	Upstream  []*Upstream `yaml:"upstream"`
	BootStrap []string    `yaml:"bootstrap"`
	HostsFile string      `yaml:"hosts"`
	Cache     bool        `yaml:"cache"`
	Strategy  string      `yaml:"strategy"`
}

var (
	configFilePath string
)

func parseListener(ls []*Listener) (lis []*listener.Listener) {
	for _, l := range ls {
		newListener := &listener.Listener{
			ServiceType: l.ServiceType,
			Addr:        l.Addr,
			CertFile:    l.CertFile,
			KeyFile:     l.KeyFile,
			HttpPath:    l.HttpPath,
		}
		lis = append(lis, newListener)
	}
	return
}

func parseUpstream(ss []*Upstream) (rss []*resolver.ClientConfig) {
	for _, s := range ss {
		newUpstream := &resolver.ClientConfig{
			URL:    s.URL,
			Weight: s.Weight,
		}
		rss = append(rss, newUpstream)
	}
	return
}

func parseConfig(configFilePath string) (*Config, error) {
	config := new(Config)

	configBytes, err := ioutil.ReadFile(configFilePath)
	if err != nil {
		return nil, err
	}

	err = yaml.Unmarshal(configBytes, config)
	if err != nil {
		return nil, err
	}

	return config, nil
}

func flagParse() {
	flag.StringVarP(&configFilePath, "config", "c", "/etc/leedns/config.yaml",
		"the path of configuration file")
	flag.Parse()
}

func main() {

	flagParse()

	config, err := parseConfig(configFilePath)
	if err != nil {
		log.Printf("Parse configuration file error: %v\n", err.Error())
		return
	}

	resolverConfig := &resolver.Config{
		ClientsConfig: parseUpstream(config.Upstream),
		Cache:         config.Cache,
		Strategy:      config.Strategy,
	}
	r, err := resolver.NewResolver(resolverConfig)
	if err != nil {
		log.Println(err.Error())
		return
	}

	var bootstrap []*resolver.ClientConfig
	for _, s := range config.BootStrap {
		newServer := new(resolver.ClientConfig)
		newServer.URL = s
		bootstrap = append(bootstrap, newServer)
	}
	if len(bootstrap) == 0 {
		for _, s := range config.Upstream {
			host, _ := url.Parse(s.URL)
			ip := net.ParseIP(host.Hostname())
			if ip != nil {
				newServer := new(resolver.ClientConfig)
				newServer.URL = s.URL
				bootstrap = append(bootstrap, newServer)
			}
		}
	}
	if len(bootstrap) > 0 {
		defaultResolverConfig := &resolver.Config{
			ClientsConfig: bootstrap,
			Strategy:      "random",
		}
		defaultResolver, err := resolver.NewResolver(defaultResolverConfig)
		if err != nil {
			log.Println(err.Error())
			return
		}
		dns.SetResolver(defaultResolver)
	}

	if config.HostsFile != "" {
		hosts, err := resolver.LoadHosts(config.HostsFile)
		if err != nil {
			log.Printf("Couldn't load hosts file: %v\n", err.Error())
		} else {
			r.Hosts = hosts
			r.ListenHostsFile(config.HostsFile)
		}
	}

	listener.Start(parseListener(config.Listener), r)
}
