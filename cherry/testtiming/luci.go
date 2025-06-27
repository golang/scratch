// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// An ad-hoc tool to query test timing data from LUCI.
//
// Output CSV with the following columns:
//
//	commit hash, commit time, [builder,] status, pass duration, fail duration
//
// The "builder" column is omitted if only one builder
// is queried (the -builder flag).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	bbpb "go.chromium.org/luci/buildbucket/proto"
	"go.chromium.org/luci/common/api/gitiles"
	gpb "go.chromium.org/luci/common/proto/gitiles"
	"go.chromium.org/luci/grpc/prpc"
	rdbpb "go.chromium.org/luci/resultdb/proto/v1"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const resultDBHost = "results.api.cr.dev"
const crBuildBucketHost = "cr-buildbucket.appspot.com"
const gitilesHost = "go.googlesource.com"

// LUCIClient is a LUCI client.
type LUCIClient struct {
	HTTPClient     *http.Client
	GitilesClient  gpb.GitilesClient
	BuildsClient   bbpb.BuildsClient
	BuildersClient bbpb.BuildersClient
	ResultDBClient rdbpb.ResultDBClient

	// TraceSteps controls whether to log each step name as it's executed.
	TraceSteps bool

	nProc int
}

// NewLUCIClient creates a LUCI client.
// nProc controls concurrency. NewLUCIClient panics if nProc is non-positive.
func NewLUCIClient(nProc int) *LUCIClient {
	if nProc < 1 {
		panic(fmt.Errorf("nProc is %d, want 1 or higher", nProc))
	}
	c := new(http.Client)
	gitilesClient, err := gitiles.NewRESTClient(c, gitilesHost, false)
	if err != nil {
		log.Fatal(err)
	}
	buildsClient := bbpb.NewBuildsClient(&prpc.Client{
		C:    c,
		Host: crBuildBucketHost,
	})
	buildersClient := bbpb.NewBuildersClient(&prpc.Client{
		C:    c,
		Host: crBuildBucketHost,
	})
	resultDBClient := rdbpb.NewResultDBClient(&prpc.Client{
		C:    c,
		Host: resultDBHost,
	})
	return &LUCIClient{
		HTTPClient:     c,
		GitilesClient:  gitilesClient,
		BuildsClient:   buildsClient,
		BuildersClient: buildersClient,
		ResultDBClient: resultDBClient,
		nProc:          nProc,
	}
}

type BuilderConfigProperties struct {
	Repo     string `json:"project,omitempty"`
	GoBranch string `json:"go_branch,omitempty"`
	Target   struct {
		GOARCH string `json:"goarch,omitempty"`
		GOOS   string `json:"goos,omitempty"`
	} `json:"target"`
	KnownIssue int `json:"known_issue,omitempty"`
}

type Builder struct {
	Name string
	*BuilderConfigProperties
}

type BuildResult struct {
	ID        int64
	Status    bbpb.Status
	Commit    string    // commit hash
	Time      time.Time // commit time
	GoCommit  string    // for subrepo build, go commit hash
	BuildTime time.Time // build end time
	Builder   string
	*BuilderConfigProperties
	InvocationID string // ResultDB invocation ID
	LogURL       string // textual log of the whole run
	LogText      string
	StepLogURL   string // textual log of the (last) failed step, if any
	StepLogText  string
	Failures     []*Failure
}

type Commit struct {
	Hash string
	Time time.Time
}

type Project struct {
	Repo     string
	GoBranch string
}

type Dashboard struct {
	Project
	Builders []Builder
	Commits  []Commit
	Results  [][]*BuildResult // indexed by builder, then by commit
}

type Failure struct {
	TestID  string
	Status  rdbpb.TestStatus
	LogURL  string
	LogText string
}

// ListCommits fetches the list of commits from Gerrit.
func (c *LUCIClient) ListCommits(ctx context.Context, repo, goBranch string, since time.Time) []Commit {
	if c.TraceSteps {
		log.Println("ListCommits", repo, goBranch)
	}
	branch := "master"
	if repo == "go" {
		branch = goBranch
	}
	var commits []Commit
	var pageToken string
nextPage:
	resp, err := c.GitilesClient.Log(ctx, &gpb.LogRequest{
		Project:    repo,
		Committish: "refs/heads/" + branch,
		PageSize:   1000,
		PageToken:  pageToken,
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range resp.GetLog() {
		commitTime := c.GetCommitter().GetTime().AsTime()
		if commitTime.Before(since) {
			goto done
		}
		commits = append(commits, Commit{
			Hash: c.GetId(),
			Time: commitTime,
		})
	}
	if resp.GetNextPageToken() != "" {
		pageToken = resp.GetNextPageToken()
		goto nextPage
	}
done:
	return commits
}

// ListBuilders fetches the list of builders, on the given repo and goBranch.
// If repo and goBranch are empty, it fetches all builders.
func (c *LUCIClient) ListBuilders(ctx context.Context, repo, goBranch, builder string) ([]Builder, error) {
	if c.TraceSteps {
		log.Println("ListBuilders", repo, goBranch)
	}
	all := repo == "" && goBranch == ""
	var builders []Builder
	var pageToken string
nextPage:
	resp, err := c.BuildersClient.ListBuilders(ctx, &bbpb.ListBuildersRequest{
		Project:   "golang",
		Bucket:    "ci",
		PageSize:  1000,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, err
	}
	for _, b := range resp.GetBuilders() {
		var p BuilderConfigProperties
		json.Unmarshal([]byte(b.GetConfig().GetProperties()), &p)
		if all || (p.Repo == repo && p.GoBranch == goBranch) {
			bName := b.GetId().GetBuilder()
			if builder != "" && bName != builder { // just want one builder, skip others
				continue
			}
			builders = append(builders, Builder{bName, &p})
		}
	}
	if resp.GetNextPageToken() != "" {
		pageToken = resp.GetNextPageToken()
		goto nextPage
	}
	slices.SortFunc(builders, func(a, b Builder) int {
		return strings.Compare(a.Name, b.Name)
	})
	return builders, nil
}

// GetBuilds fetches builds from one builder.
func (c *LUCIClient) GetBuilds(ctx context.Context, builder string, since time.Time) ([]*bbpb.Build, error) {
	if c.TraceSteps {
		log.Println("GetBuilds", builder)
	}
	pred := &bbpb.BuildPredicate{
		Builder:    &bbpb.BuilderID{Project: "golang", Bucket: "ci", Builder: builder},
		CreateTime: &bbpb.TimeRange{StartTime: timestamppb.New(since)},
	}
	mask, err := fieldmaskpb.New((*bbpb.Build)(nil), "id", "builder", "output", "status", "steps", "infra", "end_time")
	if err != nil {
		return nil, err
	}
	var builds []*bbpb.Build
	var pageToken string
nextPage:
	resp, err := c.BuildsClient.SearchBuilds(ctx, &bbpb.SearchBuildsRequest{
		Predicate: pred,
		Mask:      &bbpb.BuildMask{Fields: mask},
		PageSize:  1000,
		PageToken: pageToken,
	})
	if err != nil {
		return nil, err
	}
	builds = append(builds, resp.GetBuilds()...)
	if resp.GetNextPageToken() != "" {
		pageToken = resp.GetNextPageToken()
		goto nextPage
	}
	return builds, nil
}

// ReadBoard reads the build dashboard dash, then fills in the content.
func (c *LUCIClient) ReadBoard(ctx context.Context, dash *Dashboard, builder string, since time.Time) error {
	if c.TraceSteps {
		log.Println("ReadBoard", dash.Repo, dash.GoBranch)
	}
	dash.Commits = c.ListCommits(ctx, dash.Repo, dash.GoBranch, since)
	var err error
	dash.Builders, err = c.ListBuilders(ctx, dash.Repo, dash.GoBranch, builder)
	if err != nil {
		return err
	}

	dashMap := make([]map[string]*BuildResult, len(dash.Builders)) // indexed by builder, then keyed by commit hash

	// Get builds from builders.
	g, groupContext := errgroup.WithContext(ctx)
	g.SetLimit(c.nProc)
	for i, builder := range dash.Builders {
		builder := builder
		buildMap := make(map[string]*BuildResult)
		dashMap[i] = buildMap
		g.Go(func() error {
			bName := builder.Name
			builds, err := c.GetBuilds(groupContext, bName, since)
			if err != nil {
				return err
			}
			for _, b := range builds {
				id := b.GetId()
				var commit, goCommit string
				prop := b.GetOutput().GetProperties().GetFields()
				for _, s := range prop["sources"].GetListValue().GetValues() {
					fm := s.GetStructValue().GetFields()
					gc := fm["gitilesCommit"]
					if gc == nil {
						gc = fm["gitiles_commit"]
					}
					x := gc.GetStructValue().GetFields()
					c := x["id"].GetStringValue()
					switch repo := x["project"].GetStringValue(); repo {
					case dash.Repo:
						commit = c
					case "go":
						goCommit = c
					default:
						log.Printf("repo mismatch: %s %s %s", repo, dash.Repo, buildURL(id))
					}
				}
				if commit == "" {
					switch b.GetStatus() {
					case bbpb.Status_SUCCESS:
						log.Printf("empty commit: %s", buildURL(id))
						fallthrough
					default:
						// unfinished build, or infra failure, ignore
						continue
					}
				}
				buildTime := b.GetEndTime().AsTime()
				if r0 := buildMap[commit]; r0 != nil {
					// A build already exists for the same builder and commit.
					// Maybe manually retried, or different go commits on same subrepo commit.
					// Pick the one ended at later time.
					const printDup = false
					if printDup {
						fmt.Printf("skip duplicate build: %s %s %d %d\n", bName, shortHash(commit), id, r0.ID)
					}
					if buildTime.Before(r0.BuildTime) {
						continue
					}
				}
				rdb := b.GetInfra().GetResultdb()
				if rdb.GetHostname() != resultDBHost {
					log.Fatalf("ResultDB host mismatch: %s %s %s", rdb.GetHostname(), resultDBHost, buildURL(id))
				}
				if b.GetBuilder().GetBuilder() != bName { // sanity check
					log.Fatalf("builder mismatch: %s %s %s", b.GetBuilder().GetBuilder(), bName, buildURL(id))
				}
				r := &BuildResult{
					ID:                      id,
					Status:                  b.GetStatus(),
					Commit:                  commit,
					GoCommit:                goCommit,
					BuildTime:               buildTime,
					Builder:                 bName,
					BuilderConfigProperties: builder.BuilderConfigProperties,
					InvocationID:            rdb.GetInvocation(),
				}
				if r.Status == bbpb.Status_FAILURE {
					links := prop["failure"].GetStructValue().GetFields()["links"].GetListValue().GetValues()
					for _, l := range links {
						m := l.GetStructValue().GetFields()
						if strings.Contains(m["name"].GetStringValue(), "(combined output)") {
							r.LogURL = m["url"].GetStringValue()
							break
						}
					}
					if r.LogURL == "" {
						// No log URL, Probably a build failure.
						// E.g. https://ci.chromium.org/ui/b/8759448820419452721
						// Use the build's stderr instead.
						for _, l := range b.GetOutput().GetLogs() {
							if l.GetName() == "stderr" {
								r.LogURL = l.GetViewUrl()
								break
							}
						}
					}

					// Fetch the stderr of the failed step.
					steps := b.GetSteps()
				stepLoop:
					for i := len(steps) - 1; i >= 0; i-- {
						s := steps[i]
						if s.GetStatus() == bbpb.Status_FAILURE {
							for _, l := range s.GetLogs() {
								if l.GetName() == "stderr" || l.GetName() == "output" {
									r.StepLogURL = l.GetViewUrl()
									break stepLoop
								}
							}
						}
					}
				}
				buildMap[commit] = r
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}

	// Gather into dashboard.
	dash.Results = make([][]*BuildResult, len(dash.Builders))
	for i, m := range dashMap {
		dash.Results[i] = make([]*BuildResult, len(dash.Commits))
		for j, c := range dash.Commits {
			r := m[c.Hash]
			if r == nil {
				continue
			}
			r.Time = c.Time // fill in commit time
			dash.Results[i][j] = r
		}
	}

	return nil
}

func buildURL(buildID int64) string { // keep in sync with buildUrlRE in github.go
	return fmt.Sprintf("https://ci.chromium.org/b/%d", buildID)
}

func shortHash(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

var (
	repo    = flag.String("repo", "go", "repo name (defualt: \"go\")")
	branch  = flag.String("branch", "master", "branch (defualt: \"master\")")
	builder = flag.String("builder", "", "builder to query, if unset, query all builders")
	test    = flag.String("test", "", "test name")
)

func main() {
	flag.Parse()
	if *test == "" {
		flag.Usage()
		log.Fatal("test name unset")
	}

	ctx := context.Background()
	c := NewLUCIClient(1)
	c.TraceSteps = true

	// LUCI keeps data up to 60 days, so there is no point to go back farther
	startTime := time.Now().Add(-60 * 24 * time.Hour)
	dash := &Dashboard{Project: Project{*repo, *branch}}
	c.ReadBoard(ctx, dash, *builder, startTime)

	printBuilder := func(string) {}
	if len(dash.Builders) > 1 {
		printBuilder = func(s string) { fmt.Print(s, ",") }
	}
	for i, b := range dash.Builders {
		for _, r := range dash.Results[i] {
			if r == nil {
				continue
			}
			if c.TraceSteps {
				log.Println("QueryTestResultsRequest", b.Name, shortHash(r.Commit), r.Time)
			}
			req := &rdbpb.QueryTestResultsRequest{
				Invocations: []string{r.InvocationID},
				Predicate: &rdbpb.TestResultPredicate{
					TestIdRegexp: regexp.QuoteMeta(*test),
				},
			}
			resp, err := c.ResultDBClient.QueryTestResults(ctx, req)
			if err != nil {
				log.Fatal(err)
			}

			for _, rr := range resp.GetTestResults() {
				status := rr.GetStatus()
				if status == rdbpb.TestStatus_SKIP {
					continue
				}
				dur := rr.GetDuration().AsDuration()
				fmt.Print(shortHash(r.Commit), ",", r.Time, ",")
				printBuilder(b.Name)
				fmt.Print(status, ",")
				// Split pass and fail results so it is easy to plot them in
				// different colors.
				if status == rdbpb.TestStatus_PASS {
					fmt.Print(dur.Seconds(), ",")
				} else {
					fmt.Print(",", dur.Seconds())
				}
				fmt.Println()
			}
		}
	}
}
