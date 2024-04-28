package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"log/slog"

	"github.com/PuerkitoBio/goquery"
	"github.com/avast/retry-go/v4"
	"github.com/lmittmann/tint"
)

type Link struct {
	url         string
	page        int
	is_continue bool
}

type Detail struct {
	url   string
	page  int
	title string
}

var BASE_URL = "https://pjt3591oo.github.io"

// var BASE_URL = "http://localhost:3000"

var w = os.Stderr

// create a new logger
var logger = slog.New(tint.NewHandler(w, nil))

func link(wg *sync.WaitGroup) (context.Context, <-chan Link) {
	links := make(chan Link, 1)
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)

	go func() {
		defer close(links)
		defer wg.Done()

		for page := 1; ; page++ {
			ctx, _ := context.WithTimeout(ctx, 1*time.Second)

			c := &http.Client{}

			url := BASE_URL
			if page != 1 {
				url = fmt.Sprintf("%s/page%d", BASE_URL, page)
			}
			logger.Info("target page", "url", url)

			req, err := http.NewRequest(http.MethodGet, url, nil)
			if err != nil {
				logger.Error("problem", "from", "link", "type", "NewRequest", "err", err)
				cancel()
				return
			}

			var res *http.Response

			configs := []retry.Option{
				retry.Attempts(uint(3)),
				retry.OnRetry(func(n uint, err error) {
					logger.Error("problem", "from", "link", "type", "Do", "err", err, "retry count", n)
				}),
				retry.Delay(time.Second),
			}

			retry.Do(
				func() error {
					response, err := c.Do(req.WithContext(ctx))
					res = response

					if err != nil {
						logger.Error("problem", "from", "link", "type", "Do", "err", err)
					}

					return err
				},
				configs...,
			)

			if res == nil {
				cancel()
				return
			}

			doc, _ := goquery.NewDocumentFromReader(res.Body)
			if err != nil {
				logger.Error("problem", "from", "link", "type", "NewDocumentFromReader", "err", err)
				cancel()
				return
			}

			if len(doc.Find("div.p h3").Nodes) == 0 {
				links <- Link{url, page, false}
				return
			}

			doc.Find("div.p h3").Each(func(i int, s *goquery.Selection) {
				href, _ := s.Find("a").Attr("href")
				url := BASE_URL + href
				links <- Link{url, page, true}
			})
		}
	}()

	return ctx, links
}

// 상세 페이지 데이터 송신하는 채널 반환
func detail(links <-chan Link, wg *sync.WaitGroup) (context.Context, <-chan Detail) {
	detail := make(chan Detail, 1)

	cancel_context, cancel := context.WithCancel(context.Background())

	wg.Add(1)

	go func() {
		defer wg.Done()
		defer close(detail)

		for link := range links {
			c := &http.Client{}
			timeout_context, _ := context.WithTimeout(context.Background(), 1*time.Second)

			req, err := http.NewRequest(http.MethodGet, link.url, nil)
			if err != nil {
				logger.Error("problem", "from", "detail", "type", "NewRequest", "err", err)
				cancel()
				return
			}

			res, err := c.Do(req.WithContext(timeout_context))
			if err != nil {
				logger.Error("problem", "from", "detail", "type", "Do", "err", err)
				cancel()
				return
			}

			doc, err := goquery.NewDocumentFromReader(res.Body)
			if err != nil {
				logger.Error("problem", "from", "detail", "type", "NewDocumentFromReader", "err", err)
				cancel()
				return
			}

			t := doc.Find("h1.post-title").Text()
			detail <- Detail{link.url, link.page, t}

			if !link.is_continue {
				return
			}
		}
	}()

	return cancel_context, detail
}

func configuration() (int, *sync.WaitGroup) {
	slog.SetDefault(slog.New(
		tint.NewHandler(w, &tint.Options{
			Level:      slog.LevelDebug,
			TimeFormat: time.Kitchen,
		}),
	))

	worker := runtime.NumCPU()
	runtime.GOMAXPROCS(worker)

	wg := new(sync.WaitGroup)

	return worker, wg
}

func main() {
	worker, wg := configuration()

	logger.Info("start crawler application", "WORKER", worker)
	logger.Info("target site", "URL", BASE_URL)

	link_ctx, link_ch := link(wg)
	detail_ctx, detail_ch := detail(link_ch, wg)

	for {
		select {
		case t, ok := <-detail_ch:
			logger.Info("complete", "page", t.page, "title", t.title, "url", t.url, "is_close", ok)
			if !ok {
				wg.Wait()
				logger.Info("exit crawler application", "from", "main", "reason", "complete")
				return
			}
		case <-link_ctx.Done():
			logger.Error("exit crawler application", "from", "link", "reason", link_ctx.Err())
			wg.Wait()
			return
		case <-detail_ctx.Done():
			logger.Error("exit crawler application", "from", "detail", "reason", detail_ctx.Err())
			wg.Wait()
			return
		}
	}

}
