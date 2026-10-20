package scope

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/home-operations/flate/internal/testutil"
	"github.com/home-operations/flate/test/e2e/internal/harness"
)

func TestE2E_Diff_ExternalPatch(t *testing.T) {
	for _, tc := range []struct {
		name, file, before, after, field string
	}{
		{name: "patch", file: "apps/shared/patch.yaml", before: "before", after: "after", field: "patched"},
		{name: "resource", file: "apps/base/configmap.yaml", before: "base", after: "changed", field: "key"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			repo := testutil.GitInit(t, dir)
			testutil.WriteFile(t, dir, "clusters/test/flux.yaml", `apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata: {name: flux-system, namespace: flux-system}
spec: {url: https://fixture.invalid/cluster.git}
---
`+testutil.KustomizationYAML("workloads", "flux-system", "  path: ./workloads\n  sourceRef: {kind: GitRepository, name: flux-system}\n")+"---\n"+
				testutil.KustomizationYAML("unrelated", "flux-system", "  path: ./unrelated\n  sourceRef: {kind: GitRepository, name: flux-system}\n"))
			testutil.WriteFile(t, dir, "workloads/kustomization.yaml", "resources: [app]\n")
			testutil.WriteFile(t, dir, "workloads/app/kustomization.yaml", "namespace: app\nresources: [../../apps/shared]\n")
			testutil.WriteFile(t, dir, "apps/shared/kustomization.yaml", "resources: [../base]\npatches:\n- path: patch.yaml\n")
			testutil.WriteFile(t, dir, "apps/shared/patch.yaml", `apiVersion: v1
kind: ConfigMap
metadata: {name: demo}
data: {patched: before}
`)
			testutil.WriteFile(t, dir, "apps/base/kustomization.yaml", "resources: [configmap.yaml]\n")
			testutil.WriteFile(t, dir, "apps/base/configmap.yaml", `apiVersion: v1
kind: ConfigMap
metadata: {name: demo}
data: {key: base}
`)
			testutil.WriteFile(t, dir, "unrelated/kustomization.yaml", "resources: [missing.yaml]\n")
			commit := &gogit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(0, 0)}}
			testutil.GitCommitAll(t, repo, "base", commit)
			harness.MutateFile(t, filepath.Join(dir, tc.file), tc.before, tc.after)
			testutil.GitCommitAll(t, repo, "edit", commit)

			var previous string
			for run := range 2 {
				stdout, stderr := harness.RequireCLIOK(t, "diff", "all", "--path", dir, "--base", "HEAD~1",
					"-o", "github", "--no-progress", "--concurrency", "2", "--cache-dir", t.TempDir())
				for _, want := range []string{fmt.Sprintf("@@ data.%s @@", tc.field), "- " + tc.before, "+ " + tc.after} {
					if !strings.Contains(stdout, want) {
						t.Errorf("missing %q in diff:\n%s\nstderr:\n%s", want, stdout, stderr)
					}
				}
				if strings.Contains(stdout+stderr, "unrelated") || strings.Contains(stderr, "missing.yaml") {
					t.Errorf("unrelated overlay was selected:\n%s\n%s", stdout, stderr)
				}
				if run != 0 && stdout != previous {
					t.Errorf("diff changed between runs:\nfirst:\n%s\nsecond:\n%s", previous, stdout)
				}
				previous = stdout
			}
		})
	}
}
