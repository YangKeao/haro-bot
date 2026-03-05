package skills

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
)

func syncRepo(ctx context.Context, repoURL, repoRef, repoDir string) (*git.Repository, string, error) {
	if strings.HasPrefix(repoURL, "file://") {
		return nil, "", errors.New("file protocol not allowed")
	}
	if strings.TrimSpace(repoRef) == "" {
		repoRef = "main"
	}

	repo, err := git.PlainOpen(repoDir)
	if err != nil {
		if !errors.Is(err, git.ErrRepositoryNotExists) {
			return nil, "", err
		}
		repo, err = git.PlainInit(repoDir, false)
		if err != nil {
			return nil, "", err
		}
	}

	if err := ensureRemote(repo, "origin", repoURL); err != nil {
		return nil, "", err
	}
	if err := fetchAll(ctx, repo, repoURL); err != nil {
		return nil, "", err
	}
	if err := checkoutRef(repo, repoRef); err != nil {
		return nil, "", err
	}
	head, err := repo.Head()
	if err != nil {
		return repo, "", err
	}
	return repo, head.Hash().String(), nil
}

func ensureRemote(repo *git.Repository, name, url string) error {
	remote, err := repo.Remote(name)
	if err != nil {
		if errors.Is(err, git.ErrRemoteNotFound) {
			_, err = repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}})
			return err
		}
		return err
	}
	cfg := remote.Config()
	if len(cfg.URLs) == 0 || cfg.URLs[0] != url {
		if err := repo.DeleteRemote(name); err != nil {
			return err
		}
		_, err = repo.CreateRemote(&config.RemoteConfig{Name: name, URLs: []string{url}})
		return err
	}
	return nil
}

func fetchAll(ctx context.Context, repo *git.Repository, url string) error {
	err := repo.FetchContext(ctx, &git.FetchOptions{
		RemoteName: "origin",
		RemoteURL:  url,
		Depth:      1,
		Tags:       git.AllTags,
		RefSpecs: []config.RefSpec{
			"+refs/heads/*:refs/remotes/origin/*",
			"+refs/tags/*:refs/tags/*",
		},
		Force: true,
	})
	if err != nil && !errors.Is(err, git.NoErrAlreadyUpToDate) {
		return err
	}
	return nil
}

func checkoutRef(repo *git.Repository, repoRef string) error {
	wt, err := repo.Worktree()
	if err != nil {
		return err
	}
	if isHash(repoRef) {
		return wt.Checkout(&git.CheckoutOptions{
			Hash:  plumbing.NewHash(repoRef),
			Force: true,
		})
	}
	hash, err := resolveRefHash(repo, repoRef)
	if err != nil {
		return err
	}
	return wt.Checkout(&git.CheckoutOptions{
		Hash:  hash,
		Force: true,
	})
}

func resolveRefHash(repo *git.Repository, repoRef string) (plumbing.Hash, error) {
	var candidates []plumbing.ReferenceName
	if strings.HasPrefix(repoRef, "refs/") {
		candidates = []plumbing.ReferenceName{plumbing.ReferenceName(repoRef)}
	} else {
		candidates = []plumbing.ReferenceName{
			plumbing.ReferenceName(filepath.ToSlash("refs/remotes/origin/" + repoRef)),
			plumbing.ReferenceName(filepath.ToSlash("refs/heads/" + repoRef)),
			plumbing.ReferenceName(filepath.ToSlash("refs/tags/" + repoRef)),
		}
	}
	for _, name := range candidates {
		ref, err := repo.Reference(name, true)
		if err != nil {
			continue
		}
		return peelRef(repo, ref)
	}
	return plumbing.ZeroHash, plumbing.ErrReferenceNotFound
}

func peelRef(repo *git.Repository, ref *plumbing.Reference) (plumbing.Hash, error) {
	hash := ref.Hash()
	tag, err := repo.TagObject(hash)
	if err == nil {
		return tag.Target, nil
	}
	return hash, nil
}

func isHash(ref string) bool {
	if len(ref) < 7 || len(ref) > 40 {
		return false
	}
	for _, r := range ref {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}
