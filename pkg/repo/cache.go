package repo

import (
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/adrg/xdg"
	"github.com/mholt/archives"
	"github.com/rmohr/bazeldnf/pkg/api"
	"github.com/rmohr/bazeldnf/pkg/api/bazeldnf"
	"github.com/rmohr/bazeldnf/pkg/rpm"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

type cacheHelperOpts struct {
	cacheDir string
}

var cacheHelperValues = cacheHelperOpts{}

type CacheHelper struct {
	cacheDir string
}

func NewCacheHelper(cacheDir ...string) *CacheHelper {
	if len(cacheDir) == 0 {
		cacheDir = append(cacheDir, cacheHelperValues.cacheDir)
	} else if len(cacheDir) > 1 {
		panic("too many cache directories")
	}

	logrus.Infof("Using cache directory %s", cacheDir[0])

	dir := strings.ReplaceAll(cacheDir[0], "~", "${HOME}")
	dir = os.ExpandEnv(dir)

	return &CacheHelper{
		cacheDir: dir,
	}
}

func AddCacheHelperFlags(cmd *cobra.Command) {
	cmd.Flags().StringVarP(&cacheHelperValues.cacheDir, "cache-dir", "c", xdg.CacheHome+"/bazeldnf", "Cache directory")
}

func (r *CacheHelper) LoadMetaLink(repo *bazeldnf.Repository) (*api.Metalink, error) {
	metalink := &api.Metalink{}
	if err := r.UnmarshalFromRepoDir(repo, "metalink", metalink); err != nil {
		return nil, err
	}
	return metalink, nil
}

func (r *CacheHelper) WriteToRepoDir(repo *bazeldnf.Repository, body io.Reader, name string) error {
	dir := filepath.Join(r.cacheDir, repo.Name)
	file := filepath.Join(dir, name)

	err := os.MkdirAll(dir, 0770)
	if err != nil && !os.IsExist(err) {
		return fmt.Errorf("failed to create cache directory for %s: %v", repo.Name, err)
	}
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %v", file, err)
	}
	defer f.Close()
	_, err = io.Copy(f, body)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %v", file, err)
	}
	return nil
}

func (r *CacheHelper) OpenFromRepoDir(repo *bazeldnf.Repository, name string) (*os.File, error) {
	dir := filepath.Join(r.cacheDir, repo.Name)
	file := filepath.Join(dir, name)
	logrus.Debugf("opening %s", file)
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %v", file, err)
	}
	return f, err
}

func (r *CacheHelper) UnmarshalFromRepoDir(repo *bazeldnf.Repository, name string, obj interface{}) error {
	reader, err := r.OpenFromRepoDir(repo, name)
	if err != nil {
		return err
	}
	defer reader.Close()
	return xml.NewDecoder(reader).Decode(obj)
}

func IsGzipCompressed(data []byte) bool {
	gzipHeaderSize := 10

	if len(data) < gzipHeaderSize {
		return false
	}

	gzipHeaderMagicNumber := []byte{0x1f, 0x8b}

	if bytes.Equal(data[:2], gzipHeaderMagicNumber) {
		return true
	}

	return false
}

func (r *CacheHelper) CurrentPrimary(repo *bazeldnf.Repository) (*api.Repository, error) {
	repomd := &api.Repomd{}
	if err := r.UnmarshalFromRepoDir(repo, "repomd.xml", repomd); err != nil {
		return nil, err
	}
	primary := repomd.File(api.PrimaryFileType)
	primaryName := filepath.Base(primary.Location.Href)
	file, err := r.OpenFromRepoDir(repo, primaryName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	ctx := context.TODO()
	format, stream, err := archives.Identify(ctx, primaryName, file)
	if err != nil {
		logrus.Debugf("failed to identify: %s %v", primaryName, err)
		return nil, err
	}
	defer ctx.Done()

	var decomp archives.Decompressor
	var ok bool
	if decomp, ok = format.(archives.Decompressor); !ok {
		logrus.Debugf("failed to create decompressor")
		return nil, errors.New("failed to create decompressor")
	}

	rc, err := decomp.OpenReader(stream)
	if err != nil {
		logrus.Debugf("failed to decompress: %+v", err)
		return nil, err
	}
	defer rc.Close()

	repository := &api.Repository{}
	err = xml.NewDecoder(rc).Decode(repository)
	if err != nil {
		return nil, err
	}
	logrus.Debugf("decoded")

	if len(repo.Mirrors) == 0 && repo.Metalink != "" {
		logrus.Debugf("going to load metalink")
		metalink, err := r.LoadMetaLink(repo)
		if err == nil {
			urls := []string{}
			for _, url := range metalink.Repomod().Resources.URLs {
				if url.Type == "https" {
					urls = append(urls, strings.TrimSuffix(url.Text, "repodata/repomd.xml"))
				}
				if len(urls) == 4 {
					break
				}
			}
			repo.Mirrors = urls
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	} else if len(repo.Mirrors) == 0 && repo.Baseurl != "" {
		repo.Mirrors = []string{repo.Baseurl}
	}

	for i, _ := range repository.Packages {
		repository.Packages[i].Repository = repo
	}
	return repository, nil
}

func (r *CacheHelper) CurrentFilelistsForPackages(repo *bazeldnf.Repository, arches []string, packages []*api.Package) (filelistpkgs []*api.FileListPackage, remaining []*api.Package, err error) {
	repomd := &api.Repomd{}
	if err := r.UnmarshalFromRepoDir(repo, "repomd.xml", repomd); err != nil {
		return nil, nil, err
	}
	filelists := repomd.File(api.FilelistsFileType)
	filelistsName := filepath.Base(filelists.Location.Href)
	file, err := r.OpenFromRepoDir(repo, filelistsName)
	if err != nil {
		return nil, nil, err
	}

	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()

	d := xml.NewDecoder(reader)
	pkgIndex := 0

	sort.SliceStable(packages, func(i, j int) bool {
		if packages[i].Name == packages[j].Name {
			return rpm.Compare(packages[i].Version, packages[j].Version) < 0
		}
		return packages[i].Name < packages[j].Name
	})

	for {
		if len(packages) == pkgIndex {
			break
		}
		tok, err := d.Token()
		if tok == nil || err == io.EOF {
			break
		} else if err != nil {
			return nil, nil, fmt.Errorf("Error decoding token: %s", err)
		}

		switch ty := tok.(type) {
		case xml.StartElement:
			if ty.Name.Local == "package" {
				name := ""
				arch := ""
				for _, attr := range ty.Attr {
					if attr.Name.Local == "name" {
						name = attr.Value
					} else if attr.Name.Local == "arch" {
						arch = attr.Value
					}
				}

				validArch := false
				for _, a := range arches {
					if arch == a {
						validArch = true
					}
				}
				if !validArch {
					continue
				}

				var pkg *api.FileListPackage
				for pkgIndex < len(packages) {
					currPkg := packages[pkgIndex]
					if name < currPkg.Name {
						break
					} else if currPkg.Name == name {
						if pkg == nil {
							pkg = &api.FileListPackage{}
							if err = d.DecodeElement(pkg, &ty); err != nil {
								return nil, nil, fmt.Errorf("Error decoding item: %s", err)
							}
						}
						if currPkg.String() == pkg.String() {
							pkgIndex++
							filelistpkgs = append(filelistpkgs, pkg)
						}
						break
					} else if name > currPkg.Name {
						remaining = append(remaining, currPkg)
						pkgIndex++
					}
				}
			}
		default:
		}
	}

	return filelistpkgs, remaining, nil
}

func (r *CacheHelper) CurrentPrimaries(repos *bazeldnf.Repositories, arch string) (primaries []*api.Repository, err error) {
	for i, repo := range repos.Repositories {
		if repo.Arch != arch && repo.Arch != "noarch" {
			logrus.Infof("Ignoring primary for %s - %s", repo.Name, repo.Arch)
			continue
		}
		logrus.Debugf("loading primary for %+v", repos.Repositories[i])
		primary, err := r.CurrentPrimary(&repos.Repositories[i])
		if err != nil {
			logrus.Debugf("failed to load primary %+v", err)
			return nil, err
		}
		logrus.Debugf("primary loaded")
		primaries = append(primaries, primary)
	}
	return primaries, err
}
