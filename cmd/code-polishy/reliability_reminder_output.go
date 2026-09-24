package main

import (
	"fmt"
	"io"

	"github.com/riteofstring/code-polishy/internal/engine"
)

func printReliabilityReminder(output io.Writer, reminder *engine.ReliabilityReminder) {
	if reminder == nil {
		return
	}
	fmt.Fprintln(output, "============================================================")
	fmt.Fprintln(output, "END-TO-END RELIABILITY REMINDER")
	fmt.Fprintln(output, "MATCHED MODULES:", boundedTaskStartValues(reminder.MatchedModules))
	fmt.Fprintln(output, "MATCHED PATHS:", boundedTaskStartValues(reminder.MatchedPaths))
	fmt.Fprintln(output, "POLICY:", reminder.PolicyDocument)
	fmt.Fprintln(output)
	fmt.Fprintln(output, reminder.Principle)
	for _, question := range reminder.Questions {
		fmt.Fprintln(output, "-", question)
	}
	fmt.Fprintln(output, "============================================================")
}
