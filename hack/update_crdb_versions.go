/*
Copyright 2021 The Cockroach Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/Masterminds/semver/v3"
	"gopkg.in/yaml.v3"
)

const crdbVersionsInvertedRegexp = "^v19|^v21.1.8$|latest|ubi$"
const crdbVersionsFileName = "crdb-versions.yaml"

// TODO(rail): we may need to add pagination handling in case we pass 500 versions
// Use anonymous API to get the list of published images from the RedHat Catalog.
const crdbVersionsUrl = "https://catalog.redhat.com/api/containers/v1/repositories/registry/" +
	"registry.connect.redhat.com/repository/cockroachdb/cockroach/images?" +
	"exclude=data.repositories.comparison.advisory_rpm_mapping,data.brew," +
	"data.cpe_ids,data.top_layer_id&page_size=500&page=0"
const crdbVersionsDefaultTimeout = 30
const crdbVersionsFileDescription = `#
# Supported CockroachDB versions.
#
# This file contains a list of CockroachDB versions that are supported by the
# operator. hack/crdbversions/main.go uses this list to generate various
# manifests.
# Please update this file when CockroachDB releases new versions.
#
# Generated. DO NOT EDIT. This file is created via make release/gen-templates

`

type crdbVersionsResponse struct {
	Data []struct {
		Repositories []struct {
			Tags []struct {
				Name string `json:"name"`
			} `json:"tags"`
		} `json:"repositories"`
	} `json:"data"`
}

func getData(data *crdbVersionsResponse) error {
	client := http.Client{Timeout: crdbVersionsDefaultTimeout * time.Second}
	r, err := client.Get(crdbVersionsUrl)
	if err != nil {
		return fmt.Errorf("Cannot make a get request: %s", err)
	}
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(data)
}

func getVersions(data crdbVersionsResponse) []string {
	var versions []string
	for _, data := range data.Data {
		for _, repo := range data.Repositories {
			for _, tag := range repo.Tags {
				if isValid(tag.Name) {
					versions = append(versions, tag.Name)
				}
			}
		}
	}
	return versions
}

func isValid(version string) bool {
	match, _ := regexp.MatchString(crdbVersionsInvertedRegexp, version)
	return !match
}

// sortVersions converts the slice with versions to slice with semver.Version
// sorts them and converts back to slice with version strings
func sortVersions(versions []string) []string {
	vs := make([]*semver.Version, len(versions))
	for i, r := range versions {
		v, err := semver.NewVersion(r)
		if err != nil {
			log.Fatalf("Cannot parse version : %s", err)
		}

		vs[i] = v
	}
	sort.Sort(semver.Collection(vs))

	var sortedVersions []string
	for _, v := range vs {
		sortedVersions = append(sortedVersions, v.Original())
	}
	return sortedVersions
}

// annotation tries to open bolerplate file and combine the text from it with
// file description
func annotation() []byte {
	contents, err := ioutil.ReadFile("hack/boilerplate/boilerplate.yaml.txt")
	if err != nil {
		log.Fatalf("Cannot read boilerplate file: %s", err)
	}
	return append([]byte(contents), []byte(crdbVersionsFileDescription)...)
}

func main() {
	f, err := os.Create(crdbVersionsFileName)
	if err != nil {
		log.Fatalf("Cannot create %s file: %s", crdbVersionsFileName, err)
	}
	defer f.Close()

	responseData := crdbVersionsResponse{}
	err = getData(&responseData)
	if err != nil {
		log.Fatalf("Cannot parse response: %s", err)
	}

	// Get filtered and sorted versions in yaml representation
	versions := getVersions(responseData)
	sortedVersions := sortVersions(versions)
	yamlVersions := map[string][]string{"CrdbVersions": sortedVersions}

	var b bytes.Buffer
	yamlEncoder := yaml.NewEncoder(&b)
	yamlEncoder.SetIndent(2)
	yamlEncoder.Encode(&yamlVersions)

	result := append(annotation(), b.Bytes()...)
	err = ioutil.WriteFile(crdbVersionsFileName, result, 0)
	if err != nil {
		log.Fatalf("Cannot write %s file: %s", crdbVersionsFileName, err)
	}
}
