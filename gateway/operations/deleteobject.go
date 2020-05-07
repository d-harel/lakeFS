package operations

import (
	"errors"
	"net/http"

	"github.com/treeverse/lakefs/db"
	gatewayerrors "github.com/treeverse/lakefs/gateway/errors"
	"github.com/treeverse/lakefs/permissions"
)

type DeleteObject struct{}

func (controller *DeleteObject) Action(repoId, refId, path string) permissions.Action {
	return permissions.DeleteObject(repoId)
}

func (controller *DeleteObject) HandleAbortMultipartUpload(o *PathOperation) {
	query := o.Request.URL.Query()
	uploadId := query.Get(QueryParamUploadId)

	o.Incr("abort_mpu")
	err := o.MultipartManager.Abort(o.Repo.Id, o.Path, uploadId)
	if err != nil {
		o.Log().WithError(err).Error("could not abort multipart upload")
		o.EncodeError(gatewayerrors.Codes.ToAPIErr(gatewayerrors.ErrInternalError))
		return
	}

	// done.
	o.ResponseWriter.WriteHeader(http.StatusNoContent)
}

func (controller *DeleteObject) Handle(o *PathOperation) {
	query := o.Request.URL.Query()

	_, hasUploadId := query[QueryParamUploadId]
	if hasUploadId {
		controller.HandleAbortMultipartUpload(o)
		return
	}

	o.Incr("delete_object")
	err := o.Index.DeleteObject(o.Repo.Id, o.Ref, o.Path)
	if err != nil {
		o.Log().WithError(err).Error("could not delete key")
		if !errors.Is(err, db.ErrNotFound) {
			o.EncodeError(gatewayerrors.Codes.ToAPIErr(gatewayerrors.ErrInternalError))
			return
		}
	}
	o.ResponseWriter.WriteHeader(http.StatusNoContent)
}
