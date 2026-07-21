package platform

import "testing"

func BenchmarkBuildPipelineSpec(b *testing.B) {
	stageProfiles := fixtureStageProfiles()
	executionProfile := fixtureExecProfile()

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := BuildPipelineSpec("/bench/workspace", "go-cli", stageProfiles, executionProfile); err != nil {
			b.Fatalf("BuildPipelineSpec: %v", err)
		}
	}
}

func BenchmarkOMPPipelineEmit(b *testing.B) {
	spec := skeletonSpec("/bench/workspace")
	projector := ompPipelineProjector{}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := projector.Emit(spec); err != nil {
			b.Fatalf("Emit: %v", err)
		}
	}
}

func BenchmarkCCPipelineEmit(b *testing.B) {
	spec := skeletonSpec("/bench/workspace")
	projector := ccPipelineProjector{}

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := projector.Emit(spec); err != nil {
			b.Fatalf("Emit: %v", err)
		}
	}
}
