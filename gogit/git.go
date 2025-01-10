package main

/*
	This is an in-progress replacement for git functions, using go-git.v4

	As go-git depends on a git.Repository in-memory object being passed around for add+commits to work,
	that has to be passed into the applicable functions.
*/

import (
	"errors"
	"log"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func goGitIsClean(dataDir string) error {
	repo := goGitOpen(dataDir)
	workTree, err := repo.Worktree()
	if err != nil {
		log.Println(err)
		return err
	}
	status, err := workTree.Status()
	if err != nil {
		log.Println(err)
		return err
	}
	if !status.IsClean() {
		return errors.New("goGitIsClean: Git repo is not clean")
	}
	return nil
}

func goGitOpen(dataDir string) *git.Repository {
	repo, err := git.PlainOpen(filepath.Join(dataDir, "wikidata"))
	if err != nil {
		log.Println(err)
		log.Fatalln("Error opening repo:", err)
	}
	return repo
}

func goGitAddFilepath(repo *git.Repository, path string) error {
	workTree, err := repo.Worktree()
	if err != nil {
		log.Println(err)
		return err
	}

	_, err = workTree.Add(path)
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func goGitCommitEmpty(repo *git.Repository) error {
	workTree, err := repo.Worktree()
	if err != nil {
		log.Println(err)
		return err
	}

	_, err = workTree.Commit("commit from GoWiki", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Golang Wiki",
			Email: "golangwiki@jba.io",
			When:  time.Now(),
		},
	})
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func goGitCommitWithMessage(repo *git.Repository, msg string) error {
	workTree, err := repo.Worktree()
	if err != nil {
		log.Println(err)
		return err
	}

	_, err = workTree.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Golang Wiki",
			Email: "golangwiki@jba.io",
			When:  time.Now(),
		},
	})
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

//func goGitFilelog(repo *git.Repository)
