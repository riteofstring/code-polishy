package engine

import "github.com/riteofstring/code-polishy/internal/policy"

func (engine *Engine) DesignContext(mode string, files, modules []string) ([]string, []policy.Finding, error) {
	if findings := engine.Repository.DesignDocumentFindings(); len(findings) > 0 {
		return nil, findings, nil
	}
	if len(modules) > 0 {
		documents, err := engine.Repository.DesignDocumentsForModules(modules)
		return documents, nil, err
	}
	selection, err := engine.Select(mode, files)
	if err != nil {
		return nil, nil, err
	}
	return engine.Repository.DesignDocumentsForFiles(selection.Files), nil, nil
}
