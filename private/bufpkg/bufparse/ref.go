// Copyright 2020-2025 Buf Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bufparse

import (
	"errors"
	"fmt"
	"strings"
)

// Ref is an unresolved reference to an entity.
type Ref interface {
	// String returns "registry/owner/name[:ref]".
	fmt.Stringer

	// FullName returns the full name of the .
	//
	// Always present.
	FullName() FullName
	// Ref returns the reference within the .
	//
	// May be a label or dashless commitID.
	//
	// May be empty, in which case this references the commit of the default label of the .
	Ref() string

	isRef()
}

// NewRef returns a new Ref for the given components.
func NewRef(
	registry string,
	owner string,
	name string,
	ref string,
) (Ref, error) {
	fullName, err := NewFullName(registry, owner, name)
	if err != nil {
		return nil, err
	}
	return newRef(fullName, ref)
}

// NewRefForGitURL returns a new Ref for a git URL.
// For git URLs, we use the URL as the registry, "git" as the owner,
// and the repository name as the name. The reference (branch/tag) is parsed
// from the URL if possible, otherwise defaults to main.
func NewRefForGitURL(gitURL string) Ref {
	// For git URLs, we use a special format with:
	// - registry = the git URL
	// - owner = "git"
	// - name = extracted repository name or fallback to "repository"
	// - ref = extracted branch/tag/ref or fallback to "main"

	// Extract repository name from git URL (simple extraction)
	repoName := "repository" // Default fallback
	parts := strings.Split(gitURL, "/")
	if len(parts) > 0 {
		lastPart := parts[len(parts)-1]
		// Remove .git suffix if present
		lastPart = strings.TrimSuffix(lastPart, ".git")
		// Check if we have a reference part
		if idx := strings.IndexAny(lastPart, "#@:"); idx > 0 {
			lastPart = lastPart[:idx]
		}
		if lastPart != "" {
			repoName = lastPart
		}
	}

	// Extract reference from git URL
	reference := "main" // Default to main branch
	if idx := strings.IndexAny(gitURL, "#@:"); idx >= 0 {
		reference = gitURL[idx+1:]
	}

	fullName, _ := NewFullName(gitURL, "git", repoName)
	return &ref{
		fullName:  fullName,
		reference: reference,
	}
}

// ParseRef parses a Ref from a string in the form "registry/owner/name[:ref]".
//
// Returns an error of type *ParseError if the string could not be parsed.
func ParseRef(refString string) (Ref, error) {
	// Returns ParseErrors.
	registry, owner, name, ref, err := parseRefComponents(refString)
	if err != nil {
		return nil, err
	}
	// We don't rely on constructors for ParseErrors.
	return NewRef(registry, owner, name, ref)
}

// *** PRIVATE ***

type ref struct {
	fullName  FullName
	reference string
}

func newRef(
	fullName FullName,
	reference string,
) (*ref, error) {
	if fullName == nil {
		return nil, errors.New("nil FullName when constructing Ref")
	}
	return &ref{
		fullName:  fullName,
		reference: reference,
	}, nil
}

func (m *ref) FullName() FullName {
	return m.fullName
}

func (m *ref) Ref() string {
	return m.reference
}

func (m *ref) String() string {
	if m.reference == "" {
		return m.fullName.String()
	}
	return m.fullName.String() + ":" + m.reference
}

func (*ref) isRef() {}
