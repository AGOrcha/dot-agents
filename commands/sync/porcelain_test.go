package sync

import "testing"

func TestCountPorcelainLines(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		in            string
		wantStaged    int
		wantUnstaged  int
		wantUntracked int
	}{
		{
			name:          "empty",
			in:            "",
			wantStaged:    0,
			wantUnstaged:  0,
			wantUntracked: 0,
		},
		{
			name: "staged_modified",
			in:   "M  file.go\n",
			// x='M' (not space, not ?) -> staged++; y=' ' -> not unstaged
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 0,
		},
		{
			name: "unstaged_modified",
			in:   " M file.go\n",
			// x=' ' -> not staged; y='M' -> unstaged++
			wantStaged:    0,
			wantUnstaged:  1,
			wantUntracked: 0,
		},
		{
			name:          "untracked",
			in:            "?? new.txt\n",
			wantStaged:    0,
			wantUnstaged:  0,
			wantUntracked: 1,
		},
		{
			name:          "staged_and_untracked",
			in:            "A  added.go\n?? x\n",
			wantStaged:    1,
			wantUnstaged:  0,
			wantUntracked: 1,
		},
		{
			name:          "deleted_unstaged",
			in:            " D gone.go\n",
			wantStaged:    0,
			wantUnstaged:  1,
			wantUntracked: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, u, ut := CountPorcelainLines(tc.in)
			if s != tc.wantStaged || u != tc.wantUnstaged || ut != tc.wantUntracked {
				t.Fatalf("CountPorcelainLines(%q) = (%d,%d,%d), want (%d,%d,%d)",
					tc.in, s, u, ut, tc.wantStaged, tc.wantUnstaged, tc.wantUntracked)
			}
		})
	}
}
