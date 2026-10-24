package providers

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
)

func buildBaseHeader(cookieHeader string) http.Header {
	headers := make(http.Header)
	//headers.Set("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	headers.Set("Origin", "https://gemini.google.com")
	headers.Set("Referer", "https://gemini.google.com/")
	headers.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	headers.Set("X-Same-Domain", "1")
	//headers.Set("accept-encoding", "gzip, deflate, br, zstd")
	headers.Set("accept-language", "en-US,en;q=0.9")
	headers.Set("Cache-Control", "max-age=0")
	//headers.Set("sec-ch-ua", "?0")
	//headers.Set("sec-ch-ua-platform", "")
	headers.Set("sec-ch-ua-mobile", "?0")
	headers.Set("sec-fetch-dest", "empty")
	headers.Set("sec-fetch-mode", "cors")
	headers.Set("sec-fetch-site", "same-origin")
	if cookieHeader != "" {
		headers.Set("Cookie", cookieHeader)
	}
	return headers
}

func buildBatchExecHeader_ESY5D(cookieHeader, extId string) http.Header {
	header := buildBaseHeader(cookieHeader)
	header.Add("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	extHeader := fmt.Sprintf(`[1,null,null,null,null,null,null,null,[4,5,6,8],null,null,null,null,null,1,1,"%s"]`, extId)
	header.Add("x-goog-ext-525001261-jspb", extHeader)

	header.Add("x-goog-ext-73010989-jspb", "[0]")
	header.Add("X-Same-Domain", "1")

	return header
}

func buildBatchExecHeader_c8o8Fe(cookieHeader, extId string) http.Header {
	header := buildBaseHeader(cookieHeader)
	header.Add("Content-Type", "application/x-www-form-urlencoded;charset=utf-8")
	extHeader := fmt.Sprintf(`[1,null,null,null,null,null,null,0,[4,5,6,8,4,5,6,8],null,null,null,null,null,1,1,"%s"]`, extId)
	header.Add("x-goog-ext-525001261-jspb", extHeader)

	header.Add("x-goog-ext-73010989-jspb", "[0]")
	header.Add("X-Same-Domain", "1")

	return header
}

func batchExecUrlParam(c *Client, cid, rpcId string) string {
	queryValues := url.Values{}
	queryValues.Set("rpcids", rpcId)
	queryValues.Set("source-path", "/app/"+cid)
	queryValues.Set("bl", c.buildLabel)
	queryValues.Set("f.sid", c.sessionID)
	queryValues.Set("hl", c.language)
	queryValues.Set("_reqid", fmt.Sprintf("%d", rand.Intn(90000)+10000))
	queryValues.Set("rt", "c")
	url := EndpointBatchExec + "?" + queryValues.Encode()

	return url

}

func responseCID(response *Response) string {
	if value, ok := response.Metadata["cid"]; ok {
		if cid, ok := value.(string); ok {
			return strings.TrimPrefix(cid, "c_")
		}
	}
	return ""
}
