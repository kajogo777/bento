package manifest

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

// Media types for the bento OCI artifact.
const (
	ArtifactType           = "application/vnd.bento.workspace.v1"
	ConfigMediaType        = "application/vnd.bento.config.v1+json"
	MediaTypeProject       = "application/vnd.bento.layer.project.v1.tar+gzip"
	MediaTypeAgent         = "application/vnd.bento.layer.agent.v1.tar+gzip"
	MediaTypeDeps          = "application/vnd.bento.layer.deps.v1.tar+gzip"
	MediaTypeBuildCache    = "application/vnd.bento.layer.build-cache.v1.tar+gzip"
	MediaTypeData          = "application/vnd.bento.layer.data.v1.tar+gzip"
	MediaTypeRuntime       = "application/vnd.bento.layer.runtime.v1.tar+gzip"
	MediaTypeCustom        = "application/vnd.bento.layer.custom.v1.tar+gzip"
	MediaTypeSecretsManifest = "application/vnd.bento.secrets-manifest.v1+json"
)

// FormatVersion is the current bento format version.
const FormatVersion = "0.2.0"

// wellKnownLayers maps well-known layer names to their media types.
var wellKnownLayers = map[string]string{
	"project":     MediaTypeProject,
	"agent":       MediaTypeAgent,
	"deps":        MediaTypeDeps,
	"build-cache": MediaTypeBuildCache,
	"data":        MediaTypeData,
	"runtime":     MediaTypeRuntime,
}

// MediaTypeForLayer returns the media type for a well-known layer name.
// If the name is not recognized, it returns MediaTypeCustom.
func MediaTypeForLayer(name string) string {
	if mt, ok := wellKnownLayers[name]; ok {
		return mt
	}
	return MediaTypeCustom
}
