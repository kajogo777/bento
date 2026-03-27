package manifest

import ocispec "github.com/opencontainers/image-spec/specs-go/v1"

// OCI standard annotation keys.
const (
	AnnotationCreated = "org.opencontainers.image.created"
	AnnotationTitle   = "org.opencontainers.image.title"
)

// Bento-specific annotation keys.
const (
	AnnotationCheckpointSeq     = "dev.bento.checkpoint.sequence"
	AnnotationCheckpointParent  = "dev.bento.checkpoint.parent"
	AnnotationCheckpointMessage = "dev.bento.checkpoint.message"
	AnnotationAgent             = "dev.bento.agent"
	AnnotationTask              = "dev.bento.task"
	AnnotationHarness           = "dev.bento.harness"
	AnnotationFormatVersion     = "dev.bento.format.version"
	AnnotationLayerFileCount    = "dev.bento.layer.file-count"
	AnnotationLayerChangeFreq   = "dev.bento.layer.change-frequency"
)

// Media types: we use standard OCI types for Docker/containerd compatibility.
// Bento artifacts are structurally valid OCI images (tar+gzip filesystem layers
// with an OCI image config). The artifactType field and dev.bento.* annotations
// distinguish them from regular container images.
const (
	ArtifactType    = "application/vnd.bento.workspace.v1"
	ConfigMediaType = ocispec.MediaTypeImageConfig     // application/vnd.oci.image.config.v1+json
	LayerMediaType  = ocispec.MediaTypeImageLayerGzip  // application/vnd.oci.image.layer.v1.tar+gzip
)

// Legacy bento-specific media type constants. Kept for reference; all layers
// now use the standard OCI layer media type with annotations for semantics.
const (
	MediaTypeProject    = LayerMediaType
	MediaTypeAgent      = LayerMediaType
	MediaTypeDeps       = LayerMediaType
	MediaTypeBuildCache = LayerMediaType
	MediaTypeData       = LayerMediaType
	MediaTypeRuntime    = LayerMediaType
	MediaTypeCustom     = LayerMediaType
)

// FormatVersion is the current bento format version.
const FormatVersion = "0.3.0"

// MediaTypeForLayer returns the OCI layer media type.
// Layer semantics (deps, agent, project) are carried by annotations, not media types.
func MediaTypeForLayer(_ string) string {
	return LayerMediaType
}
