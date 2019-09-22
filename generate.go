package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apparentlymart/go-versions/versions"
	git "gopkg.in/src-d/go-git.v4"
	gitConfig "gopkg.in/src-d/go-git.v4/config"
	gitPlumbing "gopkg.in/src-d/go-git.v4/plumbing"
	gitFileMode "gopkg.in/src-d/go-git.v4/plumbing/filemode"
	gitObj "gopkg.in/src-d/go-git.v4/plumbing/object"
)

func main() {
	tfRepo, err := git.PlainInit("work.git", true)
	if err == git.ErrRepositoryAlreadyExists {
		tfRepo, err = git.PlainOpen("work.git")
	}
	if err != nil {
		log.Fatalf("failed to initialize temporary git repository: %s", err)
	}

	ourRepo, err := git.PlainOpen(".")
	if err != nil {
		log.Fatalf("failed to open our own git repository: %s", err)
	}

	log.Printf("our repository %#v", ourRepo)

	cfg, err := tfRepo.Config()
	if err != nil {
		log.Fatalf("failed to read temporary repository config: %s", err)
	}
	cfg.Remotes["origin"] = &gitConfig.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://github.com/hashicorp/terraform"},
	}
	err = tfRepo.Storer.SetConfig(cfg)
	if err != nil {
		log.Fatalf("failed to change temporary repository config: %s", err)
	}

	err = tfRepo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []gitConfig.RefSpec{gitConfig.RefSpec("refs/heads/master:refs/heads/master")},
		Tags:       git.AllTags,
		Force:      true,
	})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		log.Fatalf("failed to fetch from Terraform CLI repository: %s", err)
	}

	newestVersion := versions.Unspecified
	it, err := tfRepo.Tags()
	if err != nil {
		log.Fatalf("failed to enumerate Terraform CLI repository tags: %s", err)
	}
	err = it.ForEach(func(ref *gitPlumbing.Reference) error {
		name := ref.Name().Short()
		if len(name) > 0 && name[0] == 'v' {
			name = name[1:]
		}
		v, err := versions.ParseVersion(name)
		if err != nil {
			return nil
		}

		if v.GreaterThan(newestVersion) {
			newestVersion = v
		}

		return nil
	})
	if err != nil {
		log.Fatalf("failed to enumerate Terraform CLI repository tags: %s", err)
	}

	log.Printf("newest Terraform version is v%s", newestVersion)
	newestTagRef, err := tfRepo.Tag("v" + newestVersion.String())
	if err != nil {
		log.Fatalf("failed to read tag ref for Teraform v%s: %s", newestVersion, err)
	}
	newestTag, err := tfRepo.TagObject(newestTagRef.Hash())
	if err != nil {
		log.Fatalf("failed to read tag for Teraform v%s: %s", newestVersion, err)
	}
	latestStableCommit := newestTag.Target
	log.Printf("latest stable commit is %s", latestStableCommit)

	latestCommitRef, err := tfRepo.Head()
	if err != nil {
		log.Fatalf("failed to read latest master ref from Terraform repository: %s", err)
	}
	latestUnstableCommit := latestCommitRef.Hash()
	log.Printf("latest unstable commit is %s", latestUnstableCommit)

	stableProtoBlobRefs, err := readProtoVersionBlobRefs(tfRepo, latestStableCommit)
	if err != nil {
		log.Fatalf("failed to read protocol definitions from latest stable release: %s", err)
	}

	unstableProtoBlobRefs, err := readProtoVersionBlobRefs(tfRepo, latestUnstableCommit)
	if err != nil {
		log.Fatalf("failed to read protocol definitions from latest stable release: %s", err)
	}

	err = createStableTags(tfRepo, ourRepo, stableProtoBlobRefs)
	if err != nil {
		log.Fatalf("error while creating stable tags: %s", err)
	}
	createUnstableBranches(tfRepo, ourRepo, unstableProtoBlobRefs)
	if err != nil {
		log.Fatalf("error while creating unstable branches: %s", err)
	}
}

func readProtoVersionBlobRefs(repo *git.Repository, commitHash gitPlumbing.Hash) (map[versions.Version]gitPlumbing.Hash, error) {
	commit, err := repo.CommitObject(commitHash)
	if err != nil {
		return nil, fmt.Errorf("failed to read commit %s: %s", commitHash, err)
	}
	rootTree, err := commit.Tree()
	if err != nil {
		return nil, fmt.Errorf("failed to read tree from commit %s: %s", commitHash, err)
	}
	protoTree, err := rootTree.Tree("docs/plugin-protocol")
	if err != nil {
		return nil, fmt.Errorf("failed to find plugin protocol docs directory in commit %s: %s", commitHash, err)
	}

	ret := make(map[versions.Version]gitPlumbing.Hash, len(protoTree.Entries))
	for _, entry := range protoTree.Entries {
		name := entry.Name
		if !(strings.HasPrefix(name, "tfplugin") && strings.HasSuffix(name, ".proto")) {
			continue
		}

		versionStr := name[len("tfplugin"):len(name)-len(".proto")] + ".0"
		version, err := versions.ParseVersion(versionStr)
		if err != nil {
			continue
		}
		ret[version] = entry.Hash
	}

	return ret, nil
}

func createStableTags(tfRepo, ourRepo *git.Repository, blobs map[versions.Version]gitPlumbing.Hash) error {
	for v, blobHash := range blobs {
		tagName := "v" + v.String()
		if _, err := ourRepo.Tag(tagName); err == nil {
			// Tag already exists, so skip
			continue
		} else if err != git.ErrTagNotFound {
			return fmt.Errorf("can't check if tag %s is already present: %s", tagName, err)
		}

		blob, err := tfRepo.BlobObject(blobHash)
		if err != nil {
			return fmt.Errorf("can't find proto blob for v%s: %s", v, err)
		}

		modulePath := fmt.Sprintf("github.com/apparentlymart/terraform-plugin-proto-go/v%d", v.Major)
		packageDirName := fmt.Sprintf("tfplugin%d", v.Major)
		protoFileName := fmt.Sprintf("tfplugin%d.proto", v.Major)
		log.Printf("Protocol v%d.%d will have module path %s and tag name %s", v.Major, v.Minor, modulePath, tagName)

		tmpDir, err := ioutil.TempDir("work.git", "build-"+v.String())
		if err != nil {
			return fmt.Errorf("failed to create temporary build directory for v%s: %s", v, err)
		}
		defer os.RemoveAll(tmpDir)

		packageDirPath := filepath.Join(tmpDir, packageDirName)
		err = os.Mkdir(packageDirPath, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create %s package directory for v%s: %s", packageDirName, v, err)
		}

		cmd := exec.Command("go", "mod", "init", modulePath)
		cmd.Dir = tmpDir
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to initialize Go module for v%s: %s", v, err)
		}

		protoReader, err := blob.Reader()
		if err != nil {
			return fmt.Errorf("failed to read proto blob for v%s: %s", v, err)
		}
		defer protoReader.Close()

		protoFile, err := os.Create(filepath.Join(packageDirPath, protoFileName))
		if err != nil {
			return fmt.Errorf("failed to create temporary %s file for v%s: %s", protoFileName, v, err)
		}
		defer protoFile.Close()

		_, err = io.Copy(protoFile, protoReader)
		if err != nil {
			return fmt.Errorf("failed to write temporary %s file for v%s: %s", protoFileName, v, err)
		}

		cmd = exec.Command("protoc", "-I", "./", protoFileName, "--go_out=plugins=grpc:./")
		cmd.Dir = packageDirPath
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to compile protobuf schema for v%s: %s", v, err)
		}

		cmd = exec.Command("go", "mod", "tidy")
		cmd.Dir = tmpDir
		env := os.Environ()
		cmd.Env = make([]string, len(env)+1)
		cmd.Env[0] = "GOPROXY=https://proxy.golang.org/"
		copy(cmd.Env[1:], env)
		err = cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to locate Go module dependencies for v%s: %s", v, err)
		}

		treeHash, err := slurpDirIntoGitTree(tmpDir, ourRepo)
		if err != nil {
			return fmt.Errorf("failed to create new git tree for v%s: %s", v, err)
		}

		author := gitObj.Signature{
			Name:  "The Terraform Team",
			Email: "noreply@hashicorp.com",
			When:  time.Now(),
		}
		commit := &gitObj.Commit{
			TreeHash:  treeHash,
			Author:    author,
			Committer: author,
			Message:   fmt.Sprintf("Auto-generated Go module for Terraform plugin protocol v%d.%d", v.Major, v.Minor),
		}
		obj := ourRepo.Storer.NewEncodedObject()
		err = commit.Encode(obj)
		if err != nil {
			return fmt.Errorf("failed to create new git commit for v%s: %s", v, err)
		}
		commitHash, err := ourRepo.Storer.SetEncodedObject(obj)
		if err != nil {
			return fmt.Errorf("failed to write new git commit for v%s: %s", v, err)
		}

		_, err = ourRepo.CreateTag(tagName, commitHash, &git.CreateTagOptions{
			Tagger:  &commit.Author,
			Message: commit.Message,
		})
		if err != nil {
			return fmt.Errorf("failed to write tag for v%s: %s", v, err)
		}
		log.Printf("Created tag %s for v%d.%d, referring to commit %s", tagName, v.Major, v.Minor, commitHash)
	}

	return nil
}

func createUnstableBranches(tfRepo, ourRepo *git.Repository, blobs map[versions.Version]gitPlumbing.Hash) error {
	return nil
}

func slurpDirIntoGitTree(dir string, repo *git.Repository) (gitPlumbing.Hash, error) {
	entries, err := ioutil.ReadDir(dir)
	if err != nil {
		return gitPlumbing.ZeroHash, fmt.Errorf("failed to read %s: %s", dir, err)
	}

	tree := &gitObj.Tree{}
	for _, info := range entries {
		var entry gitObj.TreeEntry
		entry.Name = info.Name()
		mode, err := gitFileMode.NewFromOSFileMode(info.Mode())
		if err != nil {
			return gitPlumbing.ZeroHash, fmt.Errorf("invalid mode %s for %s: %s", info.Mode(), info.Name(), err)
		}
		entry.Mode = mode

		if info.IsDir() {
			hash, err := slurpDirIntoGitTree(filepath.Join(dir, info.Name()), repo)
			if err != nil {
				return gitPlumbing.ZeroHash, err
			}
			entry.Hash = hash
		} else {
			obj := repo.Storer.NewEncodedObject()
			obj.SetType(gitPlumbing.BlobObject)
			w, err := obj.Writer()
			if err != nil {
				return gitPlumbing.ZeroHash, fmt.Errorf("failed to create new blob object writer for %s: %s", entry.Name, err)
			}

			f, err := os.Open(filepath.Join(dir, info.Name()))
			if err != nil {
				return gitPlumbing.ZeroHash, fmt.Errorf("failed to open %s: %s", entry.Name, err)
			}
			defer f.Close()

			_, err = io.Copy(w, f)
			if err != nil {
				return gitPlumbing.ZeroHash, fmt.Errorf("failed to write %s: %s", entry.Name, err)
			}
			err = w.Close()
			if err != nil {
				return gitPlumbing.ZeroHash, fmt.Errorf("failed to close blob for %s: %s", entry.Name, err)
			}

			hash, err := repo.Storer.SetEncodedObject(obj)
			if err != nil {
				return gitPlumbing.ZeroHash, fmt.Errorf("failed to write blob for %s: %s", entry.Name, err)
			}
			entry.Hash = hash
		}

		tree.Entries = append(tree.Entries, entry)
	}

	obj := repo.Storer.NewEncodedObject()
	obj.SetType(gitPlumbing.TreeObject)
	err = tree.Encode(obj)
	if err != nil {
		return gitPlumbing.ZeroHash, fmt.Errorf("failed to encode tree for %s: %s", dir, err)
	}

	return repo.Storer.SetEncodedObject(obj)
}
