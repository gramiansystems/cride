package diff

// ChangeKind describes how a line participates in the review diff.
type ChangeKind int

const (
	ChangeNone ChangeKind = iota
	ChangeContext
	ChangeAdded
	ChangeDeleted
)

func (k ChangeKind) String() string {
	switch k {
	case ChangeContext:
		return "context"
	case ChangeAdded:
		return "added"
	case ChangeDeleted:
		return "deleted"
	default:
		return "none"
	}
}

// Changed reports whether the line is an actual edit, not just hunk context.
func (k ChangeKind) Changed() bool {
	return k == ChangeAdded || k == ChangeDeleted
}

// AnnotationStatus is the navigation-facing state of a review annotation.
type AnnotationStatus int

const (
	AnnotationNone AnnotationStatus = iota
	AnnotationOpen
	AnnotationMustFix
	AnnotationQuestion
	AnnotationResolved
	AnnotationUnresolved
)

func (s AnnotationStatus) String() string {
	switch s {
	case AnnotationOpen:
		return "open"
	case AnnotationMustFix:
		return "must-fix"
	case AnnotationQuestion:
		return "question"
	case AnnotationResolved:
		return "resolved"
	case AnnotationUnresolved:
		return "unresolved"
	default:
		return "none"
	}
}

// Annotated reports whether a result intersects any annotation state.
func (s AnnotationStatus) Annotated() bool {
	return s != AnnotationNone
}

// Open reports whether the annotation still needs reviewer attention.
func (s AnnotationStatus) Open() bool {
	switch s {
	case AnnotationOpen, AnnotationMustFix, AnnotationQuestion, AnnotationUnresolved:
		return true
	default:
		return false
	}
}

// ReviewMarkers are attached to navigation results so renderers and rankers can
// explain why a location matters in review context.
type ReviewMarkers struct {
	Changed          bool
	ChangeKind       ChangeKind
	ContainsAddition bool
	ContainsDeletion bool
	EntireAddition   bool
	EntireDeletion   bool
	Unread           bool
	Annotated        bool
	Annotation       AnnotationStatus
}

// ReviewIndex is a query layer over review state. It is intentionally read-only
// so unread tracking and annotations can remain owned by their eventual stores.
type ReviewIndex interface {
	IsChanged(path string) bool
	LineChangeKind(path string, line int) ChangeKind
	IsUnread(path string, line int) bool
	AnnotationStatus(path string, line int) AnnotationStatus
}

// UnreadIndex supplies unread-line state to StaticReviewIndex.
type UnreadIndex interface {
	IsUnread(path string, line int) bool
}

// AnnotationIndex supplies annotation state to StaticReviewIndex.
type AnnotationIndex interface {
	AnnotationStatus(path string, line int) AnnotationStatus
}

type reviewIndexConfig struct {
	unread      UnreadIndex
	annotations AnnotationIndex
}

// ReviewIndexOption wires optional review state providers into NewReviewIndex.
type ReviewIndexOption func(*reviewIndexConfig)

func WithUnreadIndex(unread UnreadIndex) ReviewIndexOption {
	return func(cfg *reviewIndexConfig) {
		cfg.unread = unread
	}
}

func WithAnnotationIndex(annotations AnnotationIndex) ReviewIndexOption {
	return func(cfg *reviewIndexConfig) {
		cfg.annotations = annotations
	}
}

// LineSet is a small adapter for tests and simple unread-state integrations.
type LineSet map[string]map[int]bool

func (s LineSet) IsUnread(path string, line int) bool {
	return s[path][line]
}

// AnnotationMap is a small adapter for tests and annotation integrations.
type AnnotationMap map[string]map[int]AnnotationStatus

func (m AnnotationMap) AnnotationStatus(path string, line int) AnnotationStatus {
	return m[path][line]
}

// StaticReviewIndex derives file and line review metadata from FileDiff values
// and delegates unread/annotation queries to optional providers.
type StaticReviewIndex struct {
	changedFiles map[string]bool
	lineKinds    map[string]map[int]ChangeKind
	unread       UnreadIndex
	annotations  AnnotationIndex
}

// NewReviewIndex builds a read-only index over the current review diff.
func NewReviewIndex(files []FileDiff, options ...ReviewIndexOption) StaticReviewIndex {
	cfg := reviewIndexConfig{}
	for _, option := range options {
		if option != nil {
			option(&cfg)
		}
	}

	idx := StaticReviewIndex{
		changedFiles: make(map[string]bool),
		lineKinds:    make(map[string]map[int]ChangeKind),
		unread:       cfg.unread,
		annotations:  cfg.annotations,
	}
	for _, file := range files {
		idx.indexFile(file)
	}
	return idx
}

func (idx *StaticReviewIndex) indexFile(file FileDiff) {
	if file.Status == FileUnchanged {
		return
	}
	path := file.Path()
	if validReviewPath(path) {
		idx.changedFiles[path] = true
	}
	if validReviewPath(file.OldPath) {
		idx.changedFiles[file.OldPath] = true
	}
	if validReviewPath(file.NewPath) {
		idx.changedFiles[file.NewPath] = true
	}

	oldPath := file.OldPath
	if !validReviewPath(oldPath) {
		oldPath = path
	}
	newPath := file.NewPath
	if !validReviewPath(newPath) {
		newPath = path
	}

	for _, hunk := range file.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case LineAdd:
				idx.setLineKind(newPath, line.NewLine, ChangeAdded)
			case LineDelete:
				idx.setLineKind(oldPath, line.OldLine, ChangeDeleted)
			case LineContext:
				idx.setLineKind(newPath, line.NewLine, ChangeContext)
				idx.setLineKind(oldPath, line.OldLine, ChangeContext)
			}
		}
	}
}

func (idx *StaticReviewIndex) setLineKind(path string, line int, kind ChangeKind) {
	if !validReviewPath(path) || line < 1 || kind == ChangeNone {
		return
	}
	if idx.lineKinds[path] == nil {
		idx.lineKinds[path] = make(map[int]ChangeKind)
	}
	if changeKindPriority(kind) < changeKindPriority(idx.lineKinds[path][line]) {
		return
	}
	idx.lineKinds[path][line] = kind
}

func changeKindPriority(kind ChangeKind) int {
	switch kind {
	case ChangeAdded, ChangeDeleted:
		return 3
	case ChangeContext:
		return 1
	default:
		return 0
	}
}

func validReviewPath(path string) bool {
	return path != "" && path != devNull
}

func (idx StaticReviewIndex) IsChanged(path string) bool {
	return idx.changedFiles[path]
}

func (idx StaticReviewIndex) LineChangeKind(path string, line int) ChangeKind {
	return idx.lineKinds[path][line]
}

func (idx StaticReviewIndex) IsUnread(path string, line int) bool {
	return idx.unread != nil && idx.unread.IsUnread(path, line)
}

func (idx StaticReviewIndex) AnnotationStatus(path string, line int) AnnotationStatus {
	if idx.annotations == nil {
		return AnnotationNone
	}
	return idx.annotations.AnnotationStatus(path, line)
}

// Markers returns the complete marker set for a source location.
func (idx StaticReviewIndex) Markers(path string, line int) ReviewMarkers {
	return MarkersForIndex(idx, path, line)
}

// MarkersForIndex computes markers for any ReviewIndex implementation.
func MarkersForIndex(idx ReviewIndex, path string, line int) ReviewMarkers {
	if idx == nil || path == "" || line < 1 {
		return ReviewMarkers{}
	}
	annotation := idx.AnnotationStatus(path, line)
	changeKind := idx.LineChangeKind(path, line)
	return ReviewMarkers{
		Changed:    changeKind.Changed(),
		ChangeKind: changeKind,
		Unread:     idx.IsUnread(path, line),
		Annotated:  annotation.Annotated(),
		Annotation: annotation,
	}
}

// ResultOrder controls whether result lists are review-ranked or source-sorted.
type ResultOrder int

const (
	ResultOrderReview ResultOrder = iota
	ResultOrderSource
)

func (o ResultOrder) String() string {
	if o == ResultOrderSource {
		return "source order"
	}
	return "review order"
}
