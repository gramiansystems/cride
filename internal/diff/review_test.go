package diff

import "testing"

const sample = `diff --git a/foo.go b/foo.go
index 1111111..2222222 100644
--- a/foo.go
+++ b/foo.go
@@ -1,4 +1,4 @@
 package foo

-func Old() {}
+func New() {}
diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3333333
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
`

func TestParseReview(t *testing.T) {
	files, err := ParseReview([]byte(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("want 2 files, got %d", len(files))
	}

	foo := files[0]
	if foo.Path() != "foo.go" {
		t.Errorf("path = %q, want foo.go", foo.Path())
	}
	if foo.Status != FileModified {
		t.Errorf("status = %v, want FileModified", foo.Status)
	}
	if foo.Added != 1 || foo.Deleted != 1 {
		t.Errorf("counts = +%d -%d, want +1 -1", foo.Added, foo.Deleted)
	}
	if len(foo.Hunks) != 1 {
		t.Fatalf("hunks = %d, want 1", len(foo.Hunks))
	}

	var add *Line
	for i := range foo.Hunks[0].Lines {
		if foo.Hunks[0].Lines[i].Kind == LineAdd {
			add = &foo.Hunks[0].Lines[i]
		}
	}
	if add == nil {
		t.Fatal("no added line found")
	}
	if add.NewLine != 3 {
		t.Errorf("added line NewLine = %d, want 3", add.NewLine)
	}

	newTxt := files[1]
	if newTxt.Status != FileAdded {
		t.Errorf("new.txt status = %v, want FileAdded", newTxt.Status)
	}
	if newTxt.Added != 2 {
		t.Errorf("new.txt added = %d, want 2", newTxt.Added)
	}
}

func TestParseReviewEmpty(t *testing.T) {
	files, err := ParseReview([]byte("   \n"))
	if err != nil {
		t.Fatal(err)
	}
	if files != nil {
		t.Errorf("want nil files, got %v", files)
	}
}
