package resolver

import (
	"io/ioutil"
	"log"
	"net"
	"regexp"
	"strings"

	"github.com/fsnotify/fsnotify"
	D "github.com/miekg/dns"
)

type Hosts map[string]*D.Msg

func loadFileToString(filePath string) (s string, err error) {
	bytes, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func splitByLines(str string) []string {
	str = strings.Replace(str, "\r", "\n", -1)
	return strings.Split(str, "\n")
}

func LoadHosts(hostsFile string) (hosts Hosts, err error) {
	hosts = make(Hosts)

	hostsString, err := loadFileToString(hostsFile)
	if err != nil {
		return hosts, err
	}
	list := splitByLines(hostsString)

	for _, item := range list {
		if match, _ := regexp.MatchString(`^\s*#`, item); match {
			continue
		}

		reg := regexp.MustCompile("\\s+")
		item = reg.ReplaceAllString(item, " ")

		ss := strings.Split(item, " ")
		if len(ss) == 0 {
			continue
		}
		for _, domain := range ss[1:] {
			ip := ss[0]

			if !strings.HasSuffix(domain, ".") {
				domain += "."
			}

			msg := new(D.Msg)

			var rr D.RR
			if !strings.Contains(ip, ":") {
				msg.SetQuestion(domain, D.TypeA)
				rr = &D.A{
					Hdr: D.RR_Header{
						Name:   domain,
						Rrtype: D.TypeA,
						Class:  D.ClassINET,
						Ttl:    86400,
					},
					A: net.ParseIP(ip),
				}
			} else {
				msg.SetQuestion(domain, D.TypeAAAA)
				rr = &D.AAAA{
					Hdr: D.RR_Header{
						Name:   domain,
						Rrtype: D.TypeAAAA,
						Class:  D.ClassINET,
						Ttl:    86400,
					},
					AAAA: net.ParseIP(ip),
				}
			}

			msg.SetEdns0(4096, false)
			msg.Answer = append(msg.Answer, rr)
			hosts[msg.Question[0].String()] = msg
		}
	}
	return
}

func listenHostsFile(r *Resolver, hostsFile string) {
	go func() {
		var err error

		watch, err := fsnotify.NewWatcher()
		if err != nil {
			log.Println("Watch file error:", err.Error())
			return
		}

		defer func() {
			if err := watch.Close(); err != nil {
				log.Println(err.Error())
			}
		}()

		if err = watch.Add(hostsFile); err != nil {
			log.Println("Watch file error:", err.Error())
			return
		}

		for {
			select {
			case ev := <-watch.Events:
				if ev.Op == fsnotify.Write || ev.Op == fsnotify.Remove {
					r.Hosts, err = LoadHosts(hostsFile)
					if err != nil {
						log.Println("Load hosts file error:", err.Error())
					}
				}
				if ev.Op == fsnotify.Remove {
					listenHostsFile(r, hostsFile)
					return
				}
			case err := <-watch.Errors:
				log.Println("Watch file error:", err.Error())
				return
			}
		}
	}()
}

func (h Hosts) queryHosts(q string) (msg *D.Msg, ok bool) {
	msg = h[q]
	if msg != nil {
		ok = true
	}
	return msg, ok
}
