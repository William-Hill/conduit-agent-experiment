package orchestrator

import (
	"fmt"

	"github.com/mjhilldigital/conduit-agent-experiment/internal/models"
)

// Policy defines the guardrails for a given experiment phase.
type Policy struct {
	MaxDifficulty    models.Difficulty  `json:"max_difficulty"`
	MaxBlastRadius   models.BlastRadius `json:"max_blast_radius"`
	AllowPush        bool               `json:"allow_push"`
	AllowMerge       bool               `json:"allow_merge"`
	RequireRationale bool               `json:"require_rationale"`
	MaxFilesChanged  int                `json:"max_files_changed"`
}

// DefaultPhase1Policy returns the hardcoded safety policy for milestone 1.
func DefaultPhase1Policy() Policy {
	return Policy{
		MaxDifficulty:    models.DifficultyL2,
		MaxBlastRadius:   models.BlastRadiusMedium,
		AllowPush:        false,
		AllowMerge:       false,
		RequireRationale: true,
		MaxFilesChanged:  10,
	}
}

// difficultyRank maps difficulty levels to comparable integers.
var difficultyRank = map[models.Difficulty]int{
	models.DifficultyL1: 1,
	models.DifficultyL2: 2,
	models.DifficultyL3: 3,
	models.DifficultyL4: 4,
}

// blastRadiusRank maps blast radius levels to comparable integers.
var blastRadiusRank = map[models.BlastRadius]int{
	models.BlastRadiusLow:    1,
	models.BlastRadiusMedium: 2,
	models.BlastRadiusHigh:   3,
}

// CheckPatchBreadth returns an error if the number of changed files exceeds the policy max.
func (p Policy) CheckPatchBreadth(numFiles int) error {
	if p.MaxFilesChanged > 0 && numFiles > p.MaxFilesChanged {
		return fmt.Errorf("patch touches %d files, exceeds policy max %d", numFiles, p.MaxFilesChanged)
	}
	return nil
}

// CheckTask returns an error if the task violates the policy.
func (p Policy) CheckTask(t models.Task) error {
	if difficultyRank[t.Difficulty] > difficultyRank[p.MaxDifficulty] {
		return fmt.Errorf("task difficulty %s exceeds policy max %s", t.Difficulty, p.MaxDifficulty)
	}
	if blastRadiusRank[t.BlastRadius] > blastRadiusRank[p.MaxBlastRadius] {
		return fmt.Errorf("task blast radius %s exceeds policy max %s", t.BlastRadius, p.MaxBlastRadius)
	}
	return nil
}
