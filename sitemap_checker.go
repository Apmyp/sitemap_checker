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
)

const (
	USER_AGENT   = "Mozilla/5.0 (compatible; Sitemapcheckerbot/0.1; +http://example.com)"
	CONC_KEY     = "CONC"
	DEFAULT_CONC = 5
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	ctx = context.WithValue(ctx, CONC_KEY, getConcArgument())
	defer cancel()

	sitemaps := []string{"https://www.mixbook.com/sitemaps/sitemap_index.cached.xml.gz"}
	smCh := processSitemaps(ctx, sitemaps)
	checkUrls(ctx, smCh)

	os.Exit(0)
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

type SitemapUrls struct {
	Urls []SitemapUrl `xml:"url"`
}

func parseSitemap(ctx context.Context, sm string, out chan<- string) {
	sitemap_content, err := fetchSitemap(sm)
	if err != nil {
		log.Fatalln(err)
		return
	}

	var sitemap_urls SitemapUrls
	if err := xml.Unmarshal(sitemap_content, &sitemap_urls); err != nil {
		log.Fatalln(err)
		return
	}

	for _, url := range sitemap_urls.Urls {
		select {
		case out <- url.Location:
		case <-ctx.Done():
			return
		}
	}
}

func fetchSitemap(sm string) ([]byte, error) {
	// return []byte(`<urlset><url><loc>https://www.mixbook.com/mother-s-day-photo-books</loc></url><url><loc>https://www.mixbook.com/father-s-day-photo-books</loc></url></urlset>`), nil
	log.Printf("Fetching sitemap: %s", sm)
	var client http.Client
	req, err := http.NewRequest("GET", sm, nil)
	if err != nil {
		return []byte{}, err
	}
	req.Header.Add("User-Agent", USER_AGENT)
	res, err := client.Do(req)
	if err != nil {
		return []byte{}, err
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return []byte{}, err
	}

	return body, nil
}

func isUrlAlive(url string) error {
	// return nil
	var client http.Client
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
	nConc := ctx.Value(CONC_KEY).(int)
	wg.Add(nConc)

	work := func() {
		defer wg.Done()
		for url := range in {
			select {
			case <-ctx.Done():
				return
			default:
				if err := isUrlAlive(url); err != nil {
					log.Fatalf("DEAD URL [%s]: %s", err, url)
					os.Exit(1)
				} else {
					log.Printf("ALIVE URL: %s", url)
				}
			}
		}
	}

	for range nConc {
		go work()
	}

	wg.Wait()
}
