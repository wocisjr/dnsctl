package main

import (
	"errors"
	"fmt"

	"github.com/go-git/go-billy/v6/memfs"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/storage/memory"
)

func main() {
	repo, err := git.Clone(memory.NewStorage(), memfs.New(), &git.CloneOptions{
		URL: "https://github.com/wocisjr/dnsctl.git",
	})
	if err != nil {
		panic(err)
	}

	w, err := repo.Worktree()
	if err != nil {
		panic(err)
	}

	err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		panic(err)
	}

	head, err := repo.Head()
	if err != nil {
		panic(err)
	}
	fmt.Println("HEAD:", head.Hash())
}
