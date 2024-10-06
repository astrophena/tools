// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

//go:build ignore

// autobump.go bumps the minor version of the Git tag (e.g., v0.1.0 -> v0.2.0).

package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
)

func main() {
	// Get the current tag.
	out, err := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if err != nil {
		log.Fatal(err)
	}
	currentTag := string(out)
	currentTag = currentTag[:len(currentTag)-1] // Remove the trailing newline.

	// Extract the version components.
	re := regexp.MustCompile(`v(\d+)\.(\d+)\.(\d+)`)
	matches := re.FindStringSubmatch(currentTag)
	if matches == nil {
		log.Fatal("Invalid tag format: ", currentTag)
	}

	major, _ := strconv.Atoi(matches[1])
	minor, _ := strconv.Atoi(matches[2])
	patch, _ := strconv.Atoi(matches[3])

	// Bump the minor version.
	minor++

	// Create the new tag.
	newTag := fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	fmt.Printf("Creating new tag: %s\n", newTag)

	cmd := exec.Command("git", "tag", newTag)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Tag created successfully.")
}
