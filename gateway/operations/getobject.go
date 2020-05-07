package operations

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/treeverse/lakefs/httputil"
	"github.com/treeverse/lakefs/logging"

	"github.com/treeverse/lakefs/db"
	"github.com/treeverse/lakefs/gateway/errors"
	ghttp "github.com/treeverse/lakefs/gateway/http"
	"github.com/treeverse/lakefs/gateway/serde"
	"github.com/treeverse/lakefs/permissions"

	"golang.org/x/xerrors"
)

type GetObject struct{}

func (controller *GetObject) Action(repoId, refId, path string) permissions.Action {
	return permissions.GetObject(repoId)
}

func (controller *GetObject) Handle(o *PathOperation) {
	o.Incr("get_object")
	query := o.Request.URL.Query()
	if _, exists := query["versioning"]; exists {
		o.EncodeResponse(serde.VersioningConfiguration{}, http.StatusOK)
		return
	}

	beforeMeta := time.Now()
	entry, err := o.Index.ReadEntryObject(o.Repo.Id, o.Ref, o.Path)
	metaTook := time.Since(beforeMeta)
	o.Log().
		WithField("took", metaTook).
		WithError(err).
		Debug("metadata operation to retrieve object done")

	if xerrors.Is(err, db.ErrNotFound) {
		// TODO: create distinction between missing repo & missing key
		o.EncodeError(errors.Codes.ToAPIErr(errors.ErrNoSuchKey))
		return
	}
	if err != nil {
		o.EncodeError(errors.Codes.ToAPIErr(errors.ErrInternalError))
		return
	}

	o.SetHeader("Last-Modified", httputil.HeaderTimestamp(entry.CreationDate))
	o.SetHeader("ETag", httputil.ETag(entry.Checksum))
	o.SetHeader("Accept-Ranges", "bytes")
	// TODO: the rest of https://docs.aws.amazon.com/en_pv/AmazonS3/latest/API/API_GetObject.html

	// now we might need the object itself
	obj, err := o.Index.ReadObject(o.Repo.Id, o.Ref, o.Path)
	if err != nil {
		o.EncodeError(errors.Codes.ToAPIErr(errors.ErrInternalError))
		return
	}

	// range query
	rangeSpec := o.Request.Header.Get("Range")
	if len(rangeSpec) > 0 {
		rng, err := ghttp.ParseHTTPRange(rangeSpec, obj.Size)
		if err != nil {
			o.Log().WithError(err).Error("failed to parse spec")
			return
		}
		//ranger, err := NewObjectRanger(rangeSpec, o.Repo.StorageNamespace, obj, o.BlockStore, o.Log())
		data, err := o.BlockStore.GetRange(o.Repo.StorageNamespace, obj.PhysicalAddress, rng.StartOffset, rng.EndOffset)
		if err == nil {
			// range query response
			expected := rng.EndOffset - rng.StartOffset + 1 // both range ends are inclusive
			o.SetHeader("Content-Range",
				fmt.Sprintf("bytes %d-%d/%d", rng.StartOffset, rng.EndOffset, obj.Size))
			o.SetHeader("Content-Length",
				fmt.Sprintf("%d", expected))
			o.ResponseWriter.WriteHeader(http.StatusOK)
			n, err := io.Copy(o.ResponseWriter, data)
			if err != nil {
				o.Log().WithError(err).Error("could not copy range to response")
				return
			}
			l := o.Log().WithFields(logging.Fields{
				"range":   rng,
				"written": n,
			})
			if n != expected {
				l.WithField("expected", expected).Error("got object range - didn't write the correct amount of bytes!?!!")
			} else {
				l.Info("read the byte range requested")
			}
			return
		}
	}

	// assemble a response body (range-less query)
	o.SetHeader("Content-Length", fmt.Sprintf("%d", obj.Size))
	data, err := o.BlockStore.Get(o.Repo.StorageNamespace, obj.PhysicalAddress)
	if err != nil {
		o.EncodeError(errors.Codes.ToAPIErr(errors.ErrInternalError))
		return
	}
	n, err := io.Copy(o.ResponseWriter, data)
	if err != nil {
		o.Log().WithError(err).Error("could not write response body for object")
	}
	if n != obj.Size {
		o.Log().Warnf("expected %d bytes, got %d bytes", obj.Size, n)
	}
}
