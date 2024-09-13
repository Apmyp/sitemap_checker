package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	USER_AGENT   = "Mozilla/5.0 (compatible; Sitemapcheckerbot/0.1; +https://github.com/Apmyp/sitemap_checker)"
	CONC_KEY     = "CONC"
	DEFAULT_CONC = 100
)

var EXIT_STATUS_CODE = 0

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, CONC_KEY, getConcArgument())
	defer cancel()

	sitemaps := []string{"https://www.mixbook.com/sitemaps/sitemap_index.cached.xml.gz"}
	smCh := processSitemaps(ctx, sitemaps)
	checkUrls(ctx, smCh)

	os.Exit(EXIT_STATUS_CODE)
}

func getConcArgument() int {
	value := os.Getenv(CONC_KEY)
	if len(value) == 0 {
		return DEFAULT_CONC
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		log.Fatalln(err)
		return DEFAULT_CONC
	}

	return number
}

func processSitemaps(ctx context.Context, sitemaps []string) <-chan string {
	out := make(chan string, ctx.Value(CONC_KEY).(int))
	go func() {
		defer close(out)
		var wg sync.WaitGroup
		wg.Add(len(sitemaps))
		for _, sm := range sitemaps {
			select {
			case <-ctx.Done():
			default:
				go func() {
					defer wg.Done()
					parseSitemap(ctx, sm, out)
				}()
			}
		}
		wg.Wait()
	}()
	return out
}

type SitemapUrl struct {
	Location string `xml:"loc"`
}

func parseSitemap(ctx context.Context, sm string, out chan<- string) {
	bodyReader, err := fetchSitemap(sm)
	if err != nil {
		log.Fatalln(err)
		return
	}

	dec := xml.NewDecoder(bodyReader)
	var url SitemapUrl
	for {
		t, _ := dec.Token()

		if t == nil {
			break
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "url" {
				dec.DecodeElement(&url, &se)
				select {
				case out <- url.Location:
				case <-ctx.Done():
					return
				}
			}
		default:
		}
	}
}

func fetchSitemap(sm string) (io.Reader, error) {
	log.Printf("Fetching sitemap: %s", sm)
	var client http.Client
	req, err := http.NewRequest("GET", sm, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("User-Agent", USER_AGENT)
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	return res.Body, nil
}

func isUrlAlive(client http.Client, url string) error {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		log.Fatalln(err)
		return err
	}
	req.Header.Add("User-Agent", USER_AGENT)
	res, err := client.Do(req)
	if err != nil {
		return err
	}

	status := res.StatusCode
	if status == 200 {
		return nil
	} else {
		return fmt.Errorf("status_code=%d", status)
	}
}

func checkUrls(ctx context.Context, in <-chan string) {
	var wg sync.WaitGroup
	client := http.Client{Timeout: 10 * time.Second}
	nConc := ctx.Value(CONC_KEY).(int)
	wg.Add(nConc)

	work := func(n int) {
		defer wg.Done()
		for url := range in {
			select {
			case <-ctx.Done():
				return
			default:
				if err := isUrlAlive(client, url); err != nil {
					log.Printf("[#%d] DEAD URL [%s]: %s", n, err, url)
					EXIT_STATUS_CODE = 1
					// } else {
					// 	log.Printf("[#%d] ALIVE URL: %s", n, url)
				}
			}
		}
	}

	for n := range nConc {
		go work(n)
	}

	wg.Wait()
}
