package main

// Where releases come from: GitHub. This file is the one part of the self-update that differs
// between hosts; update.go is the same everywhere.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const releaseRepo = "allco/" // + app

// projectURL and releasesURL are the links the help overlay shows under About.
func projectURL(app string) string  { return "https://github.com/" + releaseRepo + app }
func releasesURL(app string) string { return projectURL(app) + "/releases" }

// latestRelease asks GitHub for the newest release; /releases/latest skips pre-releases.
func latestRelease(ctx context.Context, app string) (string, error) {
	b, err := httpGet(ctx, "https://api.github.com/repos/"+releaseRepo+app+"/releases/latest")
	if err != nil {
		return "", err
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	return r.Tag, nil
}

// fetchAsset downloads one file of a release.
func fetchAsset(ctx context.Context, app, tag, file string) ([]byte, error) {
	return httpGet(ctx, "https://github.com/"+releaseRepo+app+"/releases/download/"+tag+"/"+file)
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "self-update/"+version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
