package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/docker/go-units"
	"github.com/moby/moby/client"
)

// Usage is one line of docker system df: images, containers, local volumes, build cache.
type Usage struct {
	Type        string `json:"type"`
	Total       int    `json:"total"`
	Active      int    `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

type Image struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
	Containers int    `json:"containers"`
}

type Volume struct {
	Name      string `json:"name"`
	Size      string `json:"size"`
	Links     int    `json:"links"`
	Anonymous bool   `json:"anonymous"`
}

type Disk struct {
	Usage   []Usage  `json:"usage"`
	Images  []Image  `json:"images"`
	Volumes []Volume `json:"volumes"`
}

func size(n int64) string {
	if n < 0 {
		return "N/A"
	}
	return units.HumanSizeWithPrecision(float64(n), 3)
}

func usage(kind string, total, active, sz, reclaim int64) Usage {
	r := size(reclaim)
	if sz > 0 {
		r += fmt.Sprintf(" (%d%%)", reclaim*100/sz)
	}
	return Usage{Type: kind, Total: int(total), Active: int(active), Size: size(sz), Reclaimable: r}
}

// Disk reads the space used by Docker, as docker system df -v shows it.
func (e *Engine) Disk(ctx context.Context) (*Disk, error) {
	c, err := e.Client()
	if err != nil {
		return nil, err
	}
	du, err := c.DiskUsage(ctx, client.DiskUsageOptions{Containers: true, Images: true, Volumes: true, BuildCache: true, Verbose: true})
	if err != nil {
		return nil, err
	}
	disk := &Disk{Usage: []Usage{
		usage("Images", du.Images.TotalCount, du.Images.ActiveCount, du.Images.TotalSize, du.Images.Reclaimable),
		usage("Containers", du.Containers.TotalCount, du.Containers.ActiveCount, du.Containers.TotalSize, du.Containers.Reclaimable),
		usage("Local Volumes", du.Volumes.TotalCount, du.Volumes.ActiveCount, du.Volumes.TotalSize, du.Volumes.Reclaimable),
		usage("Build Cache", du.BuildCache.TotalCount, du.BuildCache.ActiveCount, du.BuildCache.TotalSize, du.BuildCache.Reclaimable),
	}, Images: []Image{}, Volumes: []Volume{}}
	for _, im := range du.Images.Items {
		tags := im.RepoTags
		if len(tags) == 0 {
			tags = []string{"<none>:<none>"}
		}
		created := units.HumanDuration(time.Since(time.Unix(im.Created, 0))) + " ago"
		for _, t := range tags {
			i := strings.LastIndex(t, ":")
			repo, tag := t, "<none>"
			if i > 0 && !strings.Contains(t[i:], "/") {
				repo, tag = t[:i], t[i+1:]
			}
			disk.Images = append(disk.Images, Image{ID: short(im.ID), Repository: repo, Tag: tag, Size: size(im.Size), Created: created, Containers: int(im.Containers)})
		}
	}
	for _, v := range du.Volumes.Items {
		vol := Volume{Name: v.Name, Size: "N/A", Links: -1}
		if v.UsageData != nil {
			vol.Size, vol.Links = size(v.UsageData.Size), int(v.UsageData.RefCount)
		}
		_, vol.Anonymous = v.Labels["com.docker.volume.anonymous"]
		disk.Volumes = append(disk.Volumes, vol)
	}
	sort.SliceStable(disk.Volumes, func(i, j int) bool { return !disk.Volumes[i].Anonymous && disk.Volumes[j].Anonymous })
	return disk, nil
}

// Prune removes unused objects of one kind and returns the space freed.
func (e *Engine) Prune(ctx context.Context, what string) (string, error) {
	c, err := e.Client()
	if err != nil {
		return "", err
	}
	var freed uint64
	switch what {
	case "containers":
		r, err := c.ContainerPrune(ctx, client.ContainerPruneOptions{})
		if err != nil {
			return "", err
		}
		freed = r.Report.SpaceReclaimed
	case "danglingImages", "images":
		f := client.Filters{}.Add("dangling", "true")
		if what == "images" {
			f = client.Filters{}.Add("dangling", "false")
		}
		r, err := c.ImagePrune(ctx, client.ImagePruneOptions{Filters: f})
		if err != nil {
			return "", err
		}
		freed = r.Report.SpaceReclaimed
	case "anonymousVolumes", "volumes":
		r, err := c.VolumePrune(ctx, client.VolumePruneOptions{All: what == "volumes"})
		if err != nil {
			return "", err
		}
		freed = r.Report.SpaceReclaimed
	case "buildCache":
		r, err := c.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: true})
		if err != nil {
			return "", err
		}
		freed = r.Report.SpaceReclaimed
	default:
		return "", errUnknown(what)
	}
	return units.HumanSizeWithPrecision(float64(freed), 3), nil
}

// RemoveObject removes an image or a volume (not used by a container: Docker refuses otherwise).
func (e *Engine) RemoveObject(ctx context.Context, kind, id string) error {
	c, err := e.Client()
	if err != nil {
		return err
	}
	switch kind {
	case "image":
		_, err = c.ImageRemove(ctx, id, client.ImageRemoveOptions{})
	case "volume":
		_, err = c.VolumeRemove(ctx, id, client.VolumeRemoveOptions{})
	default:
		return errUnknown(kind)
	}
	return err
}
