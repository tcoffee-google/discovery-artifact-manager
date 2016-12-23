// Jsonpkger packages code fragments into JSON form.
//
// It reads a list of file names to be processed from the standard input, one file per line.
// Each file name must have the format:
//   path/to/{service}/{version}/{revision}/{id}.frag.{language_suffix} .
// The path may be absolute or relative.
// The "path/to" section may be empty and need not be consistent across files.
// Only the last four elements of the path are important.
//
// Code fragments are formatted with language-specific code formatters, if available.
//
// Processed files are written under a directory specified by the -dir command-line option.
//
// The design of reading file names from stdin is admittedly unusual in the Unix environment;
// jsonpkger uses it as a concession to practicality. Each output JSON file contains code fragments
// in all languages, collated from the various input fragment files provided during a single run.
// Consequently, it is important that each invocation of jsonpkger be provided with
// all fragment files for a given method. In most environments, the shell imposes a limit on the
// length of each command. This makes accepting input file names from the command line impractical
// as the number of APIs and languages that jsonpkger needs to process grows.
// Reading file names from stdin alleviates this concern.
//
// To invoke jsonpkger with specified file names on the command line:
//   ls -1 my_dir | jsonpkger
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"discovery-artifact-manager/common/errorlist"
	"discovery-artifact-manager/snippetgen/common/clientlib"
	"discovery-artifact-manager/snippetgen/common/fragment"
	"discovery-artifact-manager/snippetgen/common/metadata"
)

func main() {
	outputDir := flag.String("o", filepath.Join("apisnippets", "autogenerated"), "output directory for packaged snippet files")

	// TODO(vchudnov): Make this flag mandatory once clients incorporate this flag.
	generationVersion := flag.String("genver", "", "generation version to be included in fragment metadata")
	flag.Parse()

	fragments, err := readFilesFrom(os.Stdin, *generationVersion)
	if err != nil {
		log.Fatal(err)
	}

	if err = formatFragments(fragments); err != nil {
		log.Fatal(err)
	}

	if err = writeFiles(fragments, *outputDir); err != nil {
		log.Fatal(err)
	}
}

// fragmentLanguageMap represents the output file structure.
// Each fragment.Path represents a file.
// In each file is a map from the names of languages to code fragments in those languages.
type fragmentLanguageMap map[fragment.Path]map[string]*fragment.CodeFragment

// readFilesFrom reads file names from `reader`, one file per line,
// and processes the files into fragmentLanguageMap. It returns the fragmentLanguageMap along with
// any errors encountered.
func readFilesFrom(reader io.Reader, genver string) (fragmentLanguageMap, error) {
	fragments := make(fragmentLanguageMap)
	files := bufio.NewScanner(reader)
	var errlist errorlist.Errors

	for files.Scan() {
		fname := files.Text()
		path, err := fragment.ParseFileName(fname)
		if err != nil {
			errlist.Add(err)
			continue
		}

		fragLang := path.Lang.Name
		path.Lang = metadata.FragmentLanguage

		libURL, err := clientlib.LandingPage(fragLang, path.APIName, path.APIVersion)
		if err != nil {
			errlist.Add(err)
			continue
		}
		name := fmt.Sprintf("%s client library", fragLang)

		content, err := ioutil.ReadFile(fname)
		if err != nil {
			errlist.Add(err)
			continue
		}

		codeFrag, ok := fragments[path]
		if !ok {
			codeFrag = make(map[string]*fragment.CodeFragment)
			fragments[path] = codeFrag
		}
		codeFrag[fragLang] = &fragment.CodeFragment{
			GenerationDate:    metadata.Timestamp,
			GenerationVersion: genver,
			Fragment:          string(content),
			Libraries: []*fragment.LibraryInfo{
				{libURL, name},
			},
		}
	}
	if err := files.Err(); err != nil {
		errlist.Add(err)
	}
	return fragments, errlist.Error()
}

// writeFiles writes contents of `fragments` to files under `outDir`,
// returning any errors encountered.
func writeFiles(fragments fragmentLanguageMap, outDir string) error {
	var errlist errorlist.Errors

	for path, frags := range fragments {
		info := fragment.Info{
			Path: path,
			File: fragment.File{
				Format:       metadata.Format,
				APIName:      path.APIName,
				APIVersion:   path.APIVersion,
				APIRevision:  path.SnippetRevision,
				ID:           path.FragmentName,
				CodeFragment: frags,
			},
		}

		if err := info.ToFile(outDir, false); err != nil {
			errlist.Add(err)
		}
	}
	return errlist.Error()
}