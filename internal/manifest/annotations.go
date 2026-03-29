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
	AnnotationExtensions        = "dev.bento.extensions"
	AnnotationTask              = "dev.bento.task"
	AnnotationFormatVersion     = "dev.bento.format.version"
	AnnotationLayerFileCount = "dev.bento.layer.file-count"
	AnnotationExternalPaths  = "dev.bento.external.paths"
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

// Layer media type aliases — all layers use the standard OCI type.
// These exist for readability in tests and layer construction.
const (
	MediaTypeProject = LayerMediaType
	MediaTypeDeps    = LayerMediaType
)

// FormatVersion is the current bento format version.
const FormatVersion = "0.3.0"
