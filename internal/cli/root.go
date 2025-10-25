package cli

import (
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ai-search",
	Short: "AI-powered search engine",
	Long: `AI Search is a search engine built from scratch in Go that combines
traditional keyword search with semantic search capabilities.

It crawls web pages, extracts and chunks text, generates embeddings,
and provides hybrid retrieval with LLM reranking.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Add subcommands here
	rootCmd.AddCommand(crawlCmd)
	rootCmd.AddCommand(serverCmd)
}
