package specificScrapers

import (
	"hatt/assets"
	"hatt/helpers"
	"hatt/login"
	"hatt/variables"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/gocolly/colly"
	"github.com/tidwall/gjson"
)

// using go-rod instead of colly because the website checks on many things (headers/cookies etc) and I can't find the good combination of these to send requests without a real browser without being flagged
func (t T) Thotsbay() []variables.Item {

	var results []variables.Item
	c := colly.NewCollector()

	config := assets.DeserializeWebsiteConf("thotsbay.json")
	// serverGeneratedToken := map[string]string{
	// 	"name":  "_xfToken",
	// 	"value": "",
	// }
	// serverGeneratedToken["value"] = helpers.GetServerGeneratedTokens("https://thotsbay.ac/search/", []string{serverGeneratedToken["name"]})[serverGeneratedToken["name"]]

	loginSuccessfull := login.LoginBrowser("thotsbay")
	if !loginSuccessfull {
		message := variables.Item{
			Name: "error",
			Metadata: map[string]string{
				"name": "login_required",
			},
		}
		results = append(results, message)
		return results
	}

	h := &helpers.Helper{}
	tokens := h.DeserializeCredentials("thotsbay").Tokens

	// c.OnRequest(func(r *colly.Request) {
	// 	var tokensString string
	// 	for tokenName, token := range tokens {
	// 		tokensString += tokenName + "=" + token["value"] + "; "
	// 	}
	// 	tokensString = strings.TrimSuffix(tokensString, "; ")
	// 	r.Headers.Set("Cookie", tokensString)
	// 	r.Headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; rv:109.0) Gecko/20100101 Firefox/109.0")
	// })

	// c.OnResponse(func(r *colly.Response) {
	// 	fmt.Println(r)
	// })

	// // c.Visit(config.Search.Url + strings.ReplaceAll(variables.CURRENT_INPUT, " ", config.Search.SpaceReplacement))
	// formData := map[string]string{}
	// formData[config.Search.PostFields.Input] = variables.CURRENT_INPUT
	// formData[serverGeneratedToken["name"]] = serverGeneratedToken["value"]
	// formData["c[users]"] = ""
	// c.Post(config.Search.Url, formData)

	// send search request, a url is generated by the website with a unique id
	l := helpers.InstanciateBrowser()
	cookies := []*proto.NetworkCookieParam{}
	for tokenName, token := range tokens {
		cookies = append(cookies, &proto.NetworkCookieParam{
			Name:   tokenName,
			Value:  token["value"],
			Domain: config.SpecificInfo["domain"],
		})
	}

	browser := rod.New().ControlURL(l).MustConnect()
	browser.SetCookies(cookies)

	page := browser.MustPage(config.Search.Url)
	page.MustWaitLoad()
	page.MustElement(".inputList li:nth-of-type(1) input").MustClick().MustInput(variables.CURRENT_INPUT)

	// hijack the search request to get the search results url (with the unique id)
	var searchResultsUrl string
	router := browser.HijackRequests()
	var wg sync.WaitGroup
	router.MustAdd("*/search", func(ctx *rod.Hijack) {
		ctx.MustLoadResponse()
		responseBody := ctx.Response.Body()
		// if strings.Contains( gjson.Get(responseBody, "message"), "No results")
		searchResultsUrl = gjson.Get(responseBody, "redirect").Str
		// fmt.Println("url: ", searchResultsUrl)
		browser.Close()
		wg.Done()
	})
	go router.Run()
	page.HijackRequests()

	wg.Add(1)
	page.MustElement(".formSubmitRow-controls button").MustClick()
	wg.Wait()

	// var wg sync.WaitGroup
	// wg.Add(1)
	// go page.EachEvent(func(e *proto.PageLoadEventFired) {
	// 	// page loaded
	// 	wg.Done()
	// })()

	// wg.Wait()

	// if there are some search results, then we retreive them
	if searchResultsUrl != "" {

		c.OnHTML(".blockMessage--error.blockMessage--iconic", func(h *colly.HTMLElement) {
			if strings.Contains(h.Text, "You must be logged-in to do that") {
				message := variables.Item{
					Name: "error",
					Metadata: map[string]string{
						"name": "login_required",
					},
				}
				results = append(results, message)
			}
		})

		// now that the search url has been retreived, no cookies/headers etc. are needed to view the search results, so using colly instead
		itemKeys := config.Search.ItemKeys
		c.OnHTML(itemKeys.Root, func(h *colly.HTMLElement) {

			item := variables.Item{
				Name:      h.ChildText(itemKeys.Name),
				Thumbnail: "",
				Link:      h.Request.AbsoluteURL(h.ChildAttr(itemKeys.Link, "href")),
				Metadata:  map[string]string{},
			}

			if item.Name != "" {
				h.ForEach(".contentRow-minor ul li", func(index int, h *colly.HTMLElement) {
					if strings.Contains(h.Text, "Replies:") {
						item.Metadata["replies"] = h.Text
					} else if h.ChildText("time") != "" {
						item.Metadata["postedAt"] = h.ChildText("time")
					}

				})

				results = append(results, item)
			}

		})

		c.Visit(searchResultsUrl)

		// httpCookies := []*http.Cookie{}
		// for tokenName, token := range tokens {
		// 	httpCookies = append(httpCookies, &http.Cookie{
		// 		Name:   tokenName,
		// 		Value:  token["value"],
		// 		Domain: config.SpecificInfo["domain"],
		// 	})
		// }
		// domain := &url.URL{Scheme: config.SpecificInfo["domain"]}
		// hc.Jar.SetCookies(domain, httpCookies)

		for index, item := range results {
			wg.Add(1)
			go func(item variables.Item, index int) {
				pageCollector := colly.NewCollector()
				imgUrls := []string{}

				// todo : if the item's link contains "post-xxxx", then only check for images in the specific post and not in the whole thread
				pageCollector.OnHTML("img.bbImage", func(h *colly.HTMLElement) {
					// store urls in list, then loop over list and take the first image that works
					imgUrls = append(imgUrls, h.Attr("src"))
				})

				pageCollector.Visit(item.Link)

				for _, url := range imgUrls {
					imgBase64 := helpers.GetImageBase64(url, nil)
					results[index].Thumbnail = imgBase64
					wg.Done()
					return
				}
				// if no image was retreived
				wg.Done()
				return

			}(item, index)
		}
		wg.Wait()
	}

	return results
}
