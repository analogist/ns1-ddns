package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pelletier/go-toml"
	ns1 "gopkg.in/ns1/ns1-go.v2/rest"
	"gopkg.in/ns1/ns1-go.v2/rest/model/dns"
)

// Basic configuration
type Basic struct {
	Interval int
	APIKey   string
	Zone     string
}

// QueryAddress configuration
type QueryAddress struct {
	IPv4 string
	IPv6 string
}

// Domain configuration
type Domain struct {
	Name    string
	IPv4    bool
	IPv6    bool
	Replace bool
}

// Config file structure
type Config struct {
	Basic          Basic
	QueryAddresses QueryAddress
	Domains        []Domain `toml:"Domain"`
}

// GetIPAddress returns the public IP of the system running this application
func GetIPAddress(addrs QueryAddress) (ipv4, ipv6 net.IP, err error) {
	// Get IPv4 address
	res, err := http.Get(addrs.IPv4)
	if err != nil {
		return nil, nil, err
	}
	v4resp, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	err = ipv4.UnmarshalText(bytes.TrimSpace(v4resp))
	if err != nil {
		return nil, nil, err
	}
	if ipv4.To4() == nil {
		return nil, nil, errors.New("IPv4 request produced wrong output")
	}

	// Get IPv6 address
	res, err = http.Get(addrs.IPv6)
	if err != nil {
		return ipv4, nil, nil // Fail gracefully since IPv6 is not yet supported everywhere
	}
	v6resp, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, nil, err
	}
	err = ipv6.UnmarshalText(bytes.TrimSpace(v6resp))
	if err != nil {
		return nil, nil, err
	}
	if ipv6.To4() != nil {
		return nil, nil, errors.New("IPv6 request produced wrong output")
	}
	return
}

// UpdateDomains checks if DNS configuration is correct and updates it otherwise
func UpdateDomains(configuration Config, client *ns1.Client, ipv4, ipv6 string) {
	for _, domain := range configuration.Domains {
		if domain.IPv4 {
			record, _, err := client.Records.Get(configuration.Basic.Zone, domain.Name, "A")
			if err != nil {
				if err == ns1.ErrRecordMissing {
					newRecord := dns.NewRecord(configuration.Basic.Zone, domain.Name, "A")
					newRecord.TTL = 60
					newRecord.Type = "A"
					newRecord.AddAnswer(dns.NewAv4Answer(ipv4))
					_, err = client.Records.Create(newRecord)
					if err != nil {
						fmt.Printf("Failed to create missing DNS record for %s: %s\n", domain.Name, err.Error())
					}
				} else {
					fmt.Printf("Failed to get current DNS configuration for %s: %s\n", domain.Name, err.Error())
				}
			} else if record.TTL != 60 || len(record.Answers) != 1 || record.Answers[0].String() != ipv4 {
				record.Answers = nil
				record.AddAnswer(dns.NewAv4Answer(ipv4))
				record.TTL = 60
				_, err := client.Records.Update(record)
				if err != nil {
					fmt.Printf("Failed to update DNS record for %s: %s\n", domain.Name, err.Error())
				}
			}
		}
		if domain.IPv6 && ipv6 != "" {
			record, _, err := client.Records.Get(configuration.Basic.Zone, domain.Name, "AAAA")
			if err != nil {
				if err == ns1.ErrRecordMissing {
					newRecord := dns.NewRecord(configuration.Basic.Zone, domain.Name, "AAAA")
					newRecord.TTL = 60
					newRecord.Type = "AAAA"
					newRecord.AddAnswer(dns.NewAv6Answer(ipv6))
					_, err = client.Records.Create(newRecord)
					if err != nil {
						fmt.Printf("Failed to create missing DNS record for %s: %s\n", domain.Name, err.Error())
					}
				} else {
					fmt.Printf("Failed to get current DNS configuration for %s: %s\n", domain.Name, err.Error())
				}
			} else if record.TTL != 60 || len(record.Answers) != 1 || record.Answers[0].String() != ipv6 {
				record.Answers = nil
				record.AddAnswer(dns.NewAv6Answer(ipv6))
				record.TTL = 60
				_, err := client.Records.Update(record)
				if err != nil {
					fmt.Printf("Failed to update DNS record for %s: %s\n", domain.Name, err.Error())
				}
			}
		}
		if domain.Replace {
			if !domain.IPv4 {
				_, err := client.Records.Delete(configuration.Basic.Zone, domain.Name, "A")
				if err != nil && err != ns1.ErrRecordMissing {
					fmt.Printf("Failed to remove conflicting A record for %s: %s\n", domain.Name, err.Error())
				}
			}
			if !domain.IPv6 {
				_, err := client.Records.Delete(configuration.Basic.Zone, domain.Name, "AAAA")
				if err != nil && err != ns1.ErrRecordMissing {
					fmt.Printf("Failed to remove conflicting AAAA record for %s: %s\n", domain.Name, err.Error())
				}
			}
			_, err := client.Records.Delete(configuration.Basic.Zone, domain.Name, "CNAME")
			if err != nil && err != ns1.ErrRecordMissing {
				fmt.Printf("Failed to remove conflicting CNAME record for %s: %s\n", domain.Name, err.Error())
			}

		}
	}
}

func runDDNS(configuration Config, client *ns1.Client) {
	v4, v6, err := GetIPAddress(configuration.QueryAddresses)
	if err != nil {
		fmt.Printf("Failed to get IP address: %s\n", err.Error())
	} else {
		if v6 == nil {
			UpdateDomains(configuration, client, v4.String(), "")
		} else {
			UpdateDomains(configuration, client, v4.String(), v6.String())
		}
	}
}

func main() {
	var configuration Config

	// Set all command-line flags
	interval := flag.Int("i", 300, "The interval at which to check for changes (in seconds)")
	key := flag.String("k", "", "The NS1 API key to use")
	zone := flag.String("z", "example.com", "The zone in which the change should occur")
	domains := flag.String("d", "ddns.example.com,test.example.com", "The domain(s) to change, seperated by commas")
	ipv4 := flag.String("4", "https://ipv4bot.whatismyipaddress.com/", "Domain to query for IPv4 (A) Record (leave empty to disable)")
	ipv6 := flag.String("6", "https://ipv6bot.whatismyipaddress.com/", "Domain to query for IPv6 (AAAA) Record (leave empty to disable)")
	replace := flag.Bool("r", false, "Replace conflicting records (CNAME, A, AAAA")
	simple := flag.Bool("s", false, "Enable simple mode (Use command-link arguments instead of configuration file)")
	config := flag.String("c", "/etc/ns1-ddns/config.toml", "Path to the configuration file")
	flag.Parse()

	// Parse command-line flags when in simple mode
	if *simple {
		if *key == "" {
			fmt.Println("You need to specify an API key using the -k parameter in simple mode")
			os.Exit(-1)
		}
		configuration.Basic.APIKey = *key
		configuration.Basic.Interval = *interval
		if *zone == "example.com" {
			fmt.Println("You need to specify a zone using the -z parameter in simple mode")
			os.Exit(-1)
		}
		configuration.Basic.Zone = *zone
		if *domains == "ddns.example.com,test.example.com" {
			fmt.Println("You need to specify one or more comma-seperated domais using the -d parameter in simple mode")
			os.Exit(-1)
		}
		for _, domain := range strings.Split(strings.Replace(*domains, " ", "", -1), ",") {
			var d = new(Domain)
			d.Name = domain
			d.IPv4 = (*ipv4 != "")
			d.IPv6 = (*ipv6 != "")
			d.Replace = *replace
			configuration.Domains = append(configuration.Domains, *d)
		}
		configuration.QueryAddresses.IPv4 = *ipv4
		configuration.QueryAddresses.IPv6 = *ipv6
	} else { // Parse config file otherwise
		cfg, err := os.Open(*config)
		if err != nil {
			fmt.Println("Could not open config file.\nIf you created a config file please make sure it is in the default location or supply a custom location using '-c'.\nOtherwise please create config file or specify the simple flag to supply all options via command-line")
			os.Exit(-1)
		}
		decoder := toml.NewDecoder(cfg)
		err = decoder.Decode(&configuration)
		if err != nil {
			fmt.Printf("Could not parse config file: %s\n", err.Error())
			os.Exit(-1)
		}
	}

	_, _, err := GetIPAddress(configuration.QueryAddresses)
	if err != nil {
		fmt.Printf("Failed to get IP address: %s\n", err.Error())
		os.Exit(-1)
	}
	client := ns1.NewClient(&http.Client{Timeout: time.Second * 10}, ns1.SetAPIKey(configuration.Basic.APIKey))
	_, _, err = client.Zones.Get(configuration.Basic.Zone)
	if err != nil {
		if err == ns1.ErrZoneMissing {
			fmt.Println("Zone does not exist, please check your configuration")
			os.Exit(-1)
		} else {
			fmt.Printf("Failed to get zone info: %s\n", err.Error())
			os.Exit(-1)
		}
	}

	// run first time
	runDDNS(configuration, client)

	if configuration.Basic.Interval <= 0 {
		// running once only, exiting now
		os.Exit(0)
	}

	// else run repeatedly
	ticker := time.NewTicker(time.Duration(configuration.Basic.Interval) * time.Second)
	for range ticker.C {
		runDDNS(configuration, client)
	}
}

