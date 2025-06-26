package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"golang.org/x/net/html"
)

type ImageURL struct {
	URL   string
	Depth int
}

const (
	MaxDepth    = 3
	MaxWorkers  = 10
	DownloadDir = "images"
)

type Crawler struct {
	visited     map[string]bool
	visitedLock sync.Mutex
	wg          sync.WaitGroup
	urlChan     chan ImageURL
	client      *http.Client
}

func NewCrawler() *Crawler {
	return &Crawler{
		visited: make(map[string]bool),
		urlChan: make(chan ImageURL, 100),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func isImageURL(url string) bool {
	imgRegex := regexp.MustCompile(`\.(jpg|jpeg|png|gif|bmp)$`)
	return imgRegex.MatchString(url)
}

func getFilenameFromURL(url string) string {
	return filepath.Base(url)
}

func (c *Crawler) downloadImage(imgURL string) error {
	resp, err := c.client.Get(imgURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// makepath
	if err := os.MkdirAll(DownloadDir, 0755); err != nil {
		return err
	}

	filename := filepath.Join(DownloadDir, getFilenameFromURL(imgURL))
	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	// saveimage
	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	fmt.Printf("Downloaded: %s\n", filename)
	return nil
}

func (c *Crawler) parseHTML(body io.Reader, baseURL string, depth int) ([]string, []string) {
	var links []string
	var images []string

	tokenizer := html.NewTokenizer(body)
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			break
		}

		if tokenType == html.StartTagToken || tokenType == html.SelfClosingTagToken {
			token := tokenizer.Token()
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						links = append(links, attr.Val)
					}
				}
			} else if token.Data == "img" {
				for _, attr := range token.Attr {
					if attr.Key == "src" && isImageURL(attr.Val) {
						images = append(images, attr.Val)
					}
				}
			}
		}
	}

	return links, images
}

func (c *Crawler) worker() {
	defer c.wg.Done()

	for imgURL := range c.urlChan {
		if imgURL.Depth > MaxDepth {
			continue
		}

		c.visitedLock.Lock()
		if c.visited[imgURL.URL] {
			c.visitedLock.Unlock()
			continue
		}
		c.visited[imgURL.URL] = true
		c.visitedLock.Unlock()

		resp, err := c.client.Get(imgURL.URL)
		if err != nil {
			fmt.Printf("Error fetching %s: %v\n", imgURL.URL, err)
			continue
		}
		defer resp.Body.Close()

		links, images := c.parseHTML(resp.Body, imgURL.URL, imgURL.Depth)
		
		// download
		for _, img := range images {
			if err := c.downloadImage(img); err != nil {
				fmt.Printf("Error downloading %s: %v\n", img, err)
			}
		}

		for _, link := range links {
			c.urlChan <- ImageURL{URL: link, Depth: imgURL.Depth + 1}
		}
	}
}

func (c *Crawler) Start(startURL string) {
	for i := 0; i < MaxWorkers; i++ {
		c.wg.Add(1)
		go c.worker()
	}

	c.urlChan <- ImageURL{URL: startURL, Depth: 1}

	c.wg.Wait()
	close(c.urlChan)
}

func main() {
	startURL := "https://example.com"
	crawler := NewCrawler()
	fmt.Printf("Starting crawl from %s\n", startURL)
	crawler.Start(startURL)
	fmt.Println("Crawling completed")
}