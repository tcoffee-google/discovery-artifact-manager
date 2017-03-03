package common

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sync"

	"discovery-artifact-manager/common/environment"
	"discovery-artifact-manager/common/errorlist"
)

// discoURL specifies a URL for the live Discovery service index.
const discoURL = "https://www.googleapis.com/discovery/v1/apis"

type apiInfo struct {
	Name, Version, DiscoveryRestUrl string
}

// UpdateDiscos updates local Discovery doc files for all APIs indexed by the live Discovery
// service, in a top-level directory 'discoveries', which must exist.
func UpdateDiscos() error {
	root, err := environment.RepoRoot()
	if err != nil {
		return fmt.Errorf("Error finding repository root directory: %v", err)
	}
	discoPath := path.Join(root, "discoveries")
	info, err := os.Stat(discoPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("Error finding path for Discovery docs: %v", discoPath)
	}
	previous, err := ioutil.ReadDir(discoPath)
	if err != nil {
		return fmt.Errorf("Error reading path for Discovery docs: %v", discoPath)
	}

	fmt.Printf("Fetching Discovery doc index from %v ...\n", discoURL)
	response, err := http.Get(discoURL)
	if err != nil {
		return fmt.Errorf("Error fetching Discovery doc index: %v", err)
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("Error reading Discovery doc index: %v", err)
	}

	fmt.Println("Parsing Discovery doc index ...")
	var index struct {
		Items []apiInfo
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return fmt.Errorf("Error parsing Discovery doc index: %v", err)
	}
	size := len(index.Items)

	fmt.Printf("Updating local Discovery docs in %v:\n", discoPath)
	// Make Discovery doc file permissions like parent directory (no execute)
	perm := info.Mode() & 0666

	var collect sync.WaitGroup
	var errs errorlist.Errors
	errChan := make(chan error, size)
	collect.Add(1)
	go func() {
		defer collect.Done()
		for err := range errChan {
			fmt.Println(err)
			errs.Add(err)
		}
	}()

	updated := make(map[string]bool, size)
	updateChan := make(chan string, size)
	collect.Add(1)
	go func() {
		defer collect.Done()
		for file := range updateChan {
			updated[file] = true
		}
	}()

	var update sync.WaitGroup
	for _, api := range index.Items {
		update.Add(1)
		go func(api apiInfo) {
			defer update.Done()
			if err := UpdateAPI(api, discoPath, perm, updateChan); err != nil {
				errChan <- fmt.Errorf("Error updating %v %v: %v", api.Name, api.Version, err)
			}
		}(api)
	}
	update.Wait()
	close(errChan)
	close(updateChan)
	collect.Wait()
	for _, file := range previous {
		filename := file.Name()
		if !updated[filename] {
			filepath := path.Join(discoPath, filename)
			if err := os.Remove(filepath); err != nil {
				errs.Add(fmt.Errorf("Error deleting expired Discovery doc %v: %v", filepath, err))
			}
		}
	}
	return errs.Error()
}

// UpdateAPI updates the local Discovery doc file for an API indexed by the live Discovery service,
// sending the intended file name to a channel regardless of any error in the update.
func UpdateAPI(api apiInfo, discoPath string, perm os.FileMode, updateChan chan string) error {
	filename := api.Name + "." + api.Version + ".json"
	updateChan <- filename
	filepath := path.Join(discoPath, filename)

	file, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE, perm)
	if err != nil {
		return fmt.Errorf("Error creating local discovery doc file: %v", filepath)
	}
	defer file.Close()

	fmt.Printf("Updating API: %v %v ...\n", api.Name, api.Version)
	response, err := http.Get(api.DiscoveryRestUrl)
	if err != nil {
		return fmt.Errorf("Error downloading Discovery doc from %v: %v", api.DiscoveryRestUrl, err)
	}
	defer response.Body.Close()

	if _, err := io.Copy(file, response.Body); err != nil {
		return fmt.Errorf("Error writing local Discovery doc file: %v", filepath)
	}
	return nil
}