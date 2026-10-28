package catalog

import "regexp"

// CodeTCKCooperationUndeclared is the issue code of a TCK file in a
// with-<tag> folder of a spec that does not list @<tag> in cooperatesWith.
const CodeTCKCooperationUndeclared = "TCK_COOPERATION_UNDECLARED"

// cooperationFolder matches the folder of the TCK scenarios that call the
// routes of a cooperating spec: specs/<spec>/tck/with-<name>/. The generator
// copies those files only into projects whose installed tags include @<name>.
var cooperationFolder = regexp.MustCompile(`^specs/[^/]+/tck/with-([a-z][a-z0-9-]*)/.`)

// CooperationTag returns the tag ("@tenancy") that gates the spec file p, and
// true, when p sits in a specs/<spec>/tck/with-<name>/ folder.
func CooperationTag(p string) (string, bool) {
	match := cooperationFolder.FindStringSubmatch(p)
	if match == nil {
		return "", false
	}
	return "@" + match[1], true
}

// checkCooperationFolders reports every spec file in a with-<tag> folder whose
// tag the spec does not declare in cooperatesWith.
func (b *builder) checkCooperationFolders() {
	for _, spec := range b.specs {
		declared := map[string]bool{}
		for _, tag := range spec.spec.CooperatesWith {
			declared[tag] = true
		}
		for _, file := range spec.files {
			tag, gated := CooperationTag(file.Path)
			if gated && !declared[tag] {
				b.addPlain(CodeTCKCooperationUndeclared, file.Path,
					"the folder is gated by "+tag+", which "+spec.path+" does not list in cooperatesWith")
			}
		}
	}
}
