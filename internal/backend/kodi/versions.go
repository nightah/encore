package kodi

import (
	"context"
	"fmt"
	"time"

	"github.com/nightah/encore/internal/backend/kodi/kodirpc"
	"github.com/nightah/encore/internal/core/media"
)

// versionFile is the file of movie id's version to play when Kodi holds the film in more than one version (Kodi 21 and later), such as a 2160p and a 1080p file: the one media.BestCopy chooses for quality. from is the furthest any version was played to, when that's further than the chosen version's own bookmark. ok is false when the film has one version or Kodi can't list them; the film then plays as Kodi chooses.
func (b *Backend) versionFile(ctx context.Context, id int, quality media.Quality) (file string, from time.Duration, ok bool) {
	r, err := b.c.FilesGetDirectory(ctx, kodirpc.FilesGetDirectoryParams{
		Directory:  fmt.Sprintf("videodb://movies/titles/?videoversionid=-1&movieid=%d", id),
		Media:      "video",
		Properties: kodirpc.ListFieldsFiles{"file", "streamdetails", "resume"},
	})
	if err != nil {
		return "", 0, false
	}

	var (
		files  []string
		copies []media.Copy
	)

	for i := range r.Files {
		f := &r.Files[i]
		// A Kodi that doesn't know the filter may list other films; only this one's versions count.
		if f.Type != kodirpc.ListItemBaseTypeMovie || int(f.ID) != id || f.File == "" {
			continue
		}

		q, _ := videoOf(f.Streamdetails)
		files = append(files, f.File)
		copies = append(copies, media.Copy{Quality: q, Resume: resumeOf(f.Resume)})
	}

	if len(files) < 2 {
		return "", 0, false
	}

	i := media.BestCopy(quality, copies)
	if furthest := media.FurthestResume(copies); furthest > copies[i].Resume {
		from = furthest
	}

	return files[i], from, true
}
