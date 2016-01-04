package main

import (
	//"gopkg.in/libgit2/git2go.v23"
    "os/exec"
    "os"
    "fmt"
    "errors"
    "log"
    "strconv"
    //"encoding/binary"
	"strings"
	//"time"
)

// Git stuff based on https://github.com/ghthor/journal/blob/master/git/git.go

var gitPath string
//var wikiPath string

func init() {
	var err error
	gitPath, err = exec.LookPath("git")
	if err != nil {
		log.Fatal("git must be installed")
	}
}


// Construct an *exec.Cmd for `git {args}` with a workingDirectory
func gitCommand(args ...string) *exec.Cmd {
	c := exec.Command(gitPath, args...)
	c.Dir = "./md"
	return c
}

// Execute `git init {directory}` in the current workingDirectory
func gitInit() error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	return gitCommand(wd, "init").Run()
}

// Execute `git status -s` in directory
// If there is output, the directory has is dirty
func gitIsClean() error {
	c := gitCommand("status", "-s")

	o, err := c.Output()
	if err != nil {
		return err
	}

	if len(o) != 0 {
		return errors.New("directory is dirty")
	}

	return nil
}

// Execute `git add {filepath}` in workingDirectory
func gitAddFilepath(filepath string) error {
	o, err := gitCommand("add", filepath).CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git add`: %s\n%s", err.Error(), string(o)))
	}
	return nil
}

// Execute `git commit -m {msg}` in workingDirectory
func gitCommitWithMessage(msg string) error {
	o, err := gitCommand("commit", "-m", msg).CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git commit`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// Execute `git commit -m "commit from GoWiki"` in workingDirectory
func gitCommitEmpty() error {
	o, err := gitCommand("commit", "-m", "commit from GoWiki").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git commit`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// Execute `git push` in workingDirectory
func gitPush(msg string) error {
	o, err := gitCommand("push").CombinedOutput()
	if err != nil {
		return errors.New(fmt.Sprintf("error during `git push`: %s\n%s", err.Error(), string(o)))
	}

	return nil
}

// File creation time, output to UNIX time
// git log --diff-filter=A --follow --format=%at -1 -- [filename]
func gitGetCtime(filename string) (int64, error) {
    //var ctime int64
	o, err := gitCommand("log", "--diff-filter=A", "--follow", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("error during `git log --diff-filter=A --follow --format=%aD -1 --`: %s\n%s", err.Error(), string(o)))
	}
    ostring := strings.TrimSpace(string(o))
    ctime, err := strconv.ParseInt(ostring, 10, 64)
    if err != nil {
        log.Println(err)
    }
	return ctime, nil
}

// File modification time, output to UNIX time
// git log -1 --format=%at -- [filename]
func gitGetMtime(filename string) (int64, error) {
    //var mtime int64
	o, err := gitCommand("log", "--format=%at", "-1", "--", filename).Output()
	if err != nil {
		return 0, errors.New(fmt.Sprintf("error during `git log -1 --format=%aD --`: %s\n%s", err.Error(), string(o)))
	}
    ostring := strings.TrimSpace(string(o))
    mtime, err := strconv.ParseInt(ostring, 10, 64)
    if err != nil {
        log.Println(err)
    }    

	return mtime, nil
}

func main() {

    filename := "omg"
    fullfilename := "./md/" + filename

	log.Println("fullfilename: " + fullfilename)
    
    
    test, err := gitGetCtime(filename)
    if err != nil {
        log.Println(err)
    }
    log.Println(test)
    test2, err := gitGetMtime(filename)
    if err != nil {
        log.Println(err)
    }
    log.Println(test2)    

    /*
	gitadd := exec.Command("git", "add", fullfilename)
	gitaddout, err := gitadd.CombinedOutput()
	if err != nil {
		fmt.Println(fmt.Sprint(err) + ": " + string(gitaddout))
	}
	//fmt.Println(string(gitaddout))
	gitcommit := exec.Command("git", "commit", "-m", "commit from gowiki")
	gitcommitout, err := gitcommit.CombinedOutput()
	if err != nil {
		fmt.Println(fmt.Sprint(err) + ": " + string(gitcommitout))
	}
	//fmt.Println(string(gitcommitout))
	gitpush := exec.Command("git", "push")
	gitpushout, err := gitpush.CombinedOutput()
	if err != nil {
		fmt.Println(fmt.Sprint(err) + ": " + string(gitpushout))
	}
	//fmt.Println(string(gitpushout))
    */
    
    
    
    /*
	index, err := repo.Index()
	if err != nil {
		log.Fatalln(err)
	}
	defer index.Free()
	
	err = index.AddByPath(gitfilename)
	if err != nil {
		log.Fatalln(err)
	}
	
	treeID, err := index.WriteTree()
	if err != nil {
		log.Fatalln(err)
	}
	
	err = index.Write()
	if err != nil {
		log.Fatalln(err)
	}

	tree, err := repo.LookupTree(treeID)
	if err != nil {
		log.Fatalln(err)
	}
	
	message := "Wiki commit. Filename: " + fullfilename

	currentBranch, err := repo.Head()
	if err == nil && currentBranch != nil {
		currentTip, err2 := repo.LookupCommit(currentBranch.Target())
		if err2 != nil {
			log.Fatalln(err2)
		}
		_, err = repo.CreateCommit("HEAD", signature, signature, message, tree, currentTip)
	} else {
		_, err = repo.CreateCommit("HEAD", signature, signature, message, tree)
	}

	if err != nil {
		log.Fatalln(err)
	}
	
	log.Println(fullfilename + " has been saved.")
    */
    

}