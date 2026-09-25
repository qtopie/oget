package oget

import (
	"testing"
)

func TestParseHFSpec(t *testing.T) {
	tests := []struct {
		input    string
		expected HFSpec
	}{
		{
			input: "lmstudio-community/Qwen3.5-4B-GGUF:Q4_K_M",
			expected: HFSpec{
				Repo:     "lmstudio-community/Qwen3.5-4B-GGUF",
				Target:   "Q4_K_M",
				Revision: "main",
			},
		},
		{
			input: "hf:lmstudio-community/Qwen3.5-4B-GGUF:Q4_K_M",
			expected: HFSpec{
				Repo:     "lmstudio-community/Qwen3.5-4B-GGUF",
				Target:   "Q4_K_M",
				Revision: "main",
			},
		},
		{
			input: "hf://TheBloke/Llama-2-7B-Chat-GGUF:llama-2-7b-chat.Q4_K_M.gguf",
			expected: HFSpec{
				Repo:     "TheBloke/Llama-2-7B-Chat-GGUF",
				Target:   "llama-2-7b-chat.Q4_K_M.gguf",
				Revision: "main",
			},
		},
		{
			input: "https://hf-mirror.com/lmstudio-community/Qwen3.5-4B-GGUF:Q4_K_M",
			expected: HFSpec{
				Repo:     "lmstudio-community/Qwen3.5-4B-GGUF",
				Target:   "Q4_K_M",
				Revision: "main",
			},
		},
		{
			input: "https://huggingface.co/lmstudio-community/Qwen3.5-4B-GGUF/resolve/main/Qwen3.5-4B-Q4_K_M.gguf",
			expected: HFSpec{
				Repo:     "lmstudio-community/Qwen3.5-4B-GGUF",
				Target:   "Qwen3.5-4B-Q4_K_M.gguf",
				Revision: "main",
			},
		},
	}

	for _, tc := range tests {
		spec, err := ParseHFSpec(tc.input)
		if err != nil {
			t.Fatalf("ParseHFSpec(%s) failed: %v", tc.input, err)
		}
		if spec.Repo != tc.expected.Repo || spec.Target != tc.expected.Target || spec.Revision != tc.expected.Revision {
			t.Errorf("ParseHFSpec(%s) = %+v, want %+v", tc.input, spec, tc.expected)
		}
	}
}

func TestMatchHFFiles(t *testing.T) {
	files := []HFFileItem{
		{Type: "file", Path: "README.md"},
		{Type: "file", Path: "Qwen3.5-4B-Q4_K_M.gguf"},
		{Type: "file", Path: "Qwen3.5-4B-Q6_K.gguf"},
		{Type: "file", Path: "Qwen3.5-4B-Q8_0.gguf"},
		{Type: "file", Path: "mmproj-Qwen3.5-4B-BF16.gguf"},
		{Type: "file", Path: "Qwen3.5-72B-Q4_K_M-00001-of-00002.gguf"},
		{Type: "file", Path: "Qwen3.5-72B-Q4_K_M-00002-of-00002.gguf"},
	}

	// Case 1: Match Q4_K_M for 4B
	res := matchHFFiles(files[:5], "Q4_K_M")
	if len(res) != 1 || res[0].Path != "Qwen3.5-4B-Q4_K_M.gguf" {
		t.Fatalf("match Q4_K_M failed, got: %+v", res)
	}

	// Case 2: Match split files
	resSplits := matchHFFiles(files[5:], "Q4_K_M")
	if len(resSplits) != 2 {
		t.Fatalf("match split Q4_K_M failed, expected 2 files, got %d", len(resSplits))
	}

	// Case 3: Exact file name
	resExact := matchHFFiles(files, "mmproj-Qwen3.5-4B-BF16.gguf")
	if len(resExact) != 1 || resExact[0].Path != "mmproj-Qwen3.5-4B-BF16.gguf" {
		t.Fatalf("match exact file failed, got: %+v", resExact)
	}
}

func TestMatchHFFiles_ONNX(t *testing.T) {
	onnxRepoFiles := []HFFileItem{
		{Type: "file", Path: "README.md"},
		{Type: "file", Path: "config.json"},
		{Type: "file", Path: "tokenizer.json"},
		{Type: "file", Path: "onnx/model.onnx"},
		{Type: "file", Path: "onnx/model_quantized.onnx"},
		{Type: "file", Path: "onnx/model.onnx_data"},
	}

	// Case 1: Default (target="") on ONNX repo selects model.onnx (+ data)
	resDefault := matchHFFiles(onnxRepoFiles, "")
	if len(resDefault) != 2 {
		t.Fatalf("expected 2 default onnx files (model.onnx + data), got %d: %+v", len(resDefault), resDefault)
	}
	if resDefault[0].Path != "onnx/model.onnx" || resDefault[1].Path != "onnx/model.onnx_data" {
		t.Fatalf("unexpected default files: %+v", resDefault)
	}

	// Case 2: Target "onnx" selects all files under onnx/ directory
	resDir := matchHFFiles(onnxRepoFiles, "onnx")
	if len(resDir) != 3 {
		t.Fatalf("expected 3 files under onnx/ directory, got %d", len(resDir))
	}

	// Case 3: Target "model.onnx" selects onnx/model.onnx
	resExact := matchHFFiles(onnxRepoFiles, "model.onnx")
	if len(resExact) != 1 || resExact[0].Path != "onnx/model.onnx" {
		t.Fatalf("expected onnx/model.onnx, got %+v", resExact)
	}

	// Case 4: Target "all" selects all files
	resAll := matchHFFiles(onnxRepoFiles, "all")
	if len(resAll) != len(onnxRepoFiles) {
		t.Fatalf("expected all %d files, got %d", len(onnxRepoFiles), len(resAll))
	}
}

