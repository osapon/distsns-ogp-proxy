package main

import (
	"io"
	"fmt"
	"log"
	"net"
	"bytes"
	"net/http"
	"net/http/fcgi"
	"net/url"
	"html/template"
	"strings"

	"golang.org/x/net/html"
	"github.com/go-shiori/dom"
	"github.com/bradfitz/gomemcache/memcache"
	"crypto/md5"
)

func handler(writer http.ResponseWriter, serverRequest *http.Request) {
	if serverRequest.URL.Path == "/" {
		tmpl := template.Must(template.ParseFiles("index.html"))
		tmplVal := map[string]string{
			"site": serverRequest.URL.Host,
		}
		// テンプレートを描画
		if err := tmpl.ExecuteTemplate(writer, "index.html", tmplVal); err != nil {
			log.Fatal(err)
		}
		return
	}
	proxyUrl := serverRequest.URL.Scheme + ":/" + serverRequest.URL.Path

	if strings.Contains(serverRequest.Header.Get("User-Agent"), "(Mastodon/") ||
		strings.Contains(serverRequest.Header.Get("User-Agent"), "Misskey/") ||
		strings.Contains(serverRequest.Header.Get("User-Agent"), "Pleroma") {

		h := md5.New()
		io.WriteString(h, proxyUrl)
		cacheKey := fmt.Sprintf("ogp_%x", h.Sum(nil))
		mc := memcache.New("127.0.0.1:11211")
		proxyData, err := mc.Get(cacheKey)
		log.Printf("cahce key %s\n", cacheKey)
		if err == nil {
			// キャッシュを返す
			log.Printf("cache %s\n", proxyUrl)
			fmt.Fprintf(writer, string(proxyData.Value))
			return
		}
		if err == memcache.ErrCacheMiss {
			// データ取得
			clientRequest, err := http.NewRequest("GET", proxyUrl, nil)
			if err != nil {
				log.Printf("Error %s\n", err)
				return
			}
			clientRequest.Header.Set("User-Agent", "ogpproxy/0.9 +https://ogpproxy.osa-p.net")
	
			response, err := http.DefaultClient.Do(clientRequest)
			if err != nil {
				log.Printf("Error %s\n", err)
				return
			}
	
			doc, err := html.Parse(response.Body)
			if err != nil {
				log.Printf("Error %s\n", err)
				return
			}
	
			u, err := url.Parse(proxyUrl)
			replaceTagUrl(u, doc, "img")
			replaceTagUrl(u, doc, "link")
	
			html.Render(writer, doc)
			var buffer bytes.Buffer
			html.Render(&buffer, doc)
			mc.Set(&memcache.Item{Key: cacheKey, Value: []byte(buffer.String()), Expiration: 600})
	
			log.Printf("proxy %s\n", proxyUrl)
			return
		} else {
			// その他エラーはリダイレクト
			log.Printf("redirect %s\n", proxyUrl)
			http.Redirect(writer, serverRequest, proxyUrl, 301)
			return
		}
	}
	log.Printf("redirect %s\n", proxyUrl)
	http.Redirect(writer, serverRequest, proxyUrl, 301)
}

func replaceTagUrl(url *url.URL, doc *html.Node, tagName string) {
	tags := dom.GetElementsByTagName(doc, tagName)
	for _, node := range tags {
		for _, a := range node.Attr {
			if a.Key != "src" && a.Key != "href" {
				continue
			}
			if (strings.HasPrefix(a.Val, "//") || strings.HasPrefix(a.Val, "https://") || strings.HasPrefix(a.Val, "http://")) {
				// 絶対パスなのでそのまま
			} else {
				var attrUrl string = ""
				if strings.HasPrefix(a.Val, "/") {
					attrUrl = url.Scheme + "://" + url.Host + a.Val
				} else {
					attrUrl = url.Scheme + "://" + url.Host + "/" + a.Val
				}
				dom.SetAttribute(node, a.Key, attrUrl)
				log.Printf("attr %s %s\n", a.Val, attrUrl)
			}
		}
	}
}


func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)

	l, _ := net.Listen("tcp", "127.0.0.1:8080")
	http.HandleFunc("/", handler)
	fcgi.Serve(l, nil)
}
