package repository

type CandidateImpact struct {
	Paths           []string
	DirectModules   []string
	ImpactedModules []string
}

func (repo Repository) CandidateImpact(candidate CandidateDelta) CandidateImpact {
	paths := candidate.Paths()
	direct := []string{}
	for _, path := range paths {
		direct = append(direct, repo.ModuleNames(path)...)
	}
	direct = uniqueSorted(direct)
	return CandidateImpact{
		Paths:           paths,
		DirectModules:   direct,
		ImpactedModules: repo.reverseDependentModules(direct),
	}
}

func (repo Repository) reverseDependentModules(roots []string) []string {
	selected := map[string]bool{}
	queue := append([]string{}, roots...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if selected[name] {
			continue
		}
		selected[name] = true
		for _, module := range repo.Config.Modules {
			for _, dependency := range module.DependsOn {
				if dependency == name {
					queue = append(queue, module.Name)
					break
				}
			}
		}
	}
	result := make([]string, 0, len(selected))
	for name := range selected {
		result = append(result, name)
	}
	return uniqueSorted(result)
}
