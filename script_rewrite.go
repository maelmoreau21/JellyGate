//go:build ignore

package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
)

func main() {
	files, err := filepath.Glob("internal/core/*.go")
	if err != nil {
		fmt.Println("Error: ", err)
		return
	}

	for _, file := range files {
		content, err := ioutil.ReadFile(file)
		if err != nil {
			fmt.Println("Error reading: ", file)
			continue
		}

		newContent := strings.Replace(string(content), "package core", "package main", 1)

		err = ioutil.WriteFile(file, []byte(newContent), 0644)
		if err != nil {
			fmt.Println("Error writing: ", file)
		} else {
			fmt.Println("Replaced inside ", file)
		}
	}
}
