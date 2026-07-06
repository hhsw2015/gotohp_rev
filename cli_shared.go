package main

import (
	"bufio"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"app/backend"

	"github.com/charmbracelet/lipgloss"
)

//go:embed build/windows/info.json
var versionInfo embed.FS

var (
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99"))
	commandStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	flagStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	argStyle     = lipgloss.NewStyle().Italic(true).Foreground(lipgloss.Color("245"))
	exampleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

// getAppVersion returns version from embedded info.json
func getAppVersion() string {
	return backend.GetVersion(versionInfo)
}

func isCLICommand(arg string) bool {
	supportedCommands := []string{
		"upload",
		"download",
		"thumbnail", "thumb", // Get thumbnail at various sizes
		"list", "ls", // List media items
		"albums", // List albums
		"autowash", // Start auto-wash service
		"credentials", "creds", // Support both full and short form
		"login",                // One-shot oauth_token -> credentials
		"trash",                // Soft-delete by dedupKey
		"delete", "purge",      // Hard-delete by dedupKey
		"empty-trash", "empty", // Purge all items currently in trash
		"help", "--help", "-h",
		"version", "--version", "-v",
	}

	return slices.Contains(supportedCommands, arg)
}

func runCLI() {
	if len(os.Args) < 2 {
		printCLIHelp()
		os.Exit(1)
		return
	}

	command := os.Args[1]

	switch command {
	case "upload":
		// Check for help flag first
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printUploadHelp()
			return
		}

		if len(os.Args) < 3 {
			fmt.Println("Error: filepath required")
			printUploadHelp()
			os.Exit(1)
		}

		// Parse arguments
		filePath := os.Args[2]

		// Validate that filepath exists
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Error: file or directory does not exist: %s\n", filePath)
			os.Exit(1)
		}

		filePaths := []string{filePath}
		config := cliConfig{
			threads:  3,
			logLevel: "info", // Default to info for CLI
		}

		// Parse flags
		for i := 3; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--recursive", "-r":
				config.recursive = true
			case "--force", "-f":
				config.forceUpload = true
			case "--delete", "-d":
				config.deleteFromHost = true
			case "--disable-filter", "-df":
				config.disableUnsupportedFilesFilter = true
			case "--date-from-filename":
				config.setDateFromFilename = true
			case "--exclude", "-e":
				if i+1 < len(os.Args) {
					config.excludePattern = os.Args[i+1]
					i++
				}
			case "--threads", "-t":
				if i+1 < len(os.Args) {
					_, _ = fmt.Sscanf(os.Args[i+1], "%d", &config.threads)
					i++
				}
			case "--log-level", "-l":
				if i+1 < len(os.Args) {
					config.logLevel = os.Args[i+1]
					i++
				}
			case "--config", "-c":
				if i+1 < len(os.Args) {
					config.configPath = os.Args[i+1]
					i++
				}
			case "--album", "-a":
				if i+1 < len(os.Args) {
					config.albumName = os.Args[i+1]
					i++
				}
			}
		}

		// Run upload
		err := runCLIUpload(filePaths, config)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Upload failed: %v\n", err)
			os.Exit(1)
		}

	case "download":
		// Check for help flag first
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printDownloadHelp()
			return
		}

		if len(os.Args) < 3 {
			fmt.Println("Error: media-key required")
			printDownloadHelp()
			os.Exit(1)
		}

		mediaKey := os.Args[2]
		outputPath := ""
		original := false
		configPath := ""

		// Parse flags
		for i := 3; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--output", "-o":
				if i+1 < len(os.Args) {
					outputPath = os.Args[i+1]
					i++
				}
			case "--original":
				original = true
			case "--config", "-c":
				if i+1 < len(os.Args) {
					configPath = os.Args[i+1]
					i++
				}
			}
		}

		if configPath != "" {
			backend.ConfigPath = configPath
		}

		err := runCLIDownload(mediaKey, outputPath, original)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
			os.Exit(1)
		}

	case "thumbnail", "thumb":
		// Check for help flag first
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printThumbnailHelp()
			return
		}

		if len(os.Args) < 3 {
			fmt.Println("Error: media-key required")
			printThumbnailHelp()
			os.Exit(1)
		}

		mediaKey := os.Args[2]
		outputPath := ""
		width := 0
		height := 0
		size := ""
		noOverlay := true
		forceJPEG := true
		configPath := ""

		// Parse flags
		for i := 3; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--output", "-o":
				if i+1 < len(os.Args) {
					outputPath = os.Args[i+1]
					i++
				}
			case "--width", "-w":
				if i+1 < len(os.Args) {
					if _, err := fmt.Sscanf(os.Args[i+1], "%d", &width); err != nil || width <= 0 {
						fmt.Fprintf(os.Stderr, "Warning: invalid width '%s', ignoring\n", os.Args[i+1])
						width = 0
					}
					i++
				}
			case "--height", "-h":
				if i+1 < len(os.Args) {
					if _, err := fmt.Sscanf(os.Args[i+1], "%d", &height); err != nil || height <= 0 {
						fmt.Fprintf(os.Stderr, "Warning: invalid height '%s', ignoring\n", os.Args[i+1])
						height = 0
					}
					i++
				}
			case "--size", "-s":
				if i+1 < len(os.Args) {
					size = os.Args[i+1]
					i++
				}
			case "--overlay":
				noOverlay = false
			case "--png":
				forceJPEG = false
			case "--config", "-c":
				if i+1 < len(os.Args) {
					configPath = os.Args[i+1]
					i++
				}
			}
		}

		if configPath != "" {
			backend.ConfigPath = configPath
		}

		err := runCLIThumbnail(mediaKey, outputPath, width, height, size, noOverlay, forceJPEG)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Thumbnail download failed: %v\n", err)
			os.Exit(1)
		}

		case "list", "ls":
		// Check for help flag first
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printListHelp()
			return
		}

			// Parse flags
			configPath := ""
			pages := 1          // Default to 1 page
			maxEmptyPages := 10 // Default max empty page retries
			pageToken := ""
			jsonOutput := false

		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
				case "--config", "-c":
					if i+1 < len(os.Args) {
						configPath = os.Args[i+1]
						i++
					}
				case "--pages":
					if i+1 < len(os.Args) {
						_, err := fmt.Sscanf(os.Args[i+1], "%d", &pages)
						if err != nil || pages < 1 {
						fmt.Fprintf(os.Stderr, "Warning: invalid pages value '%s', using 1\n", os.Args[i+1])
						pages = 1
					}
					i++
				}
			case "--max-empty-pages":
				if i+1 < len(os.Args) {
					_, err := fmt.Sscanf(os.Args[i+1], "%d", &maxEmptyPages)
					if err != nil || maxEmptyPages < 1 {
						fmt.Fprintf(os.Stderr, "Warning: invalid max-empty-pages value '%s', using 10\n", os.Args[i+1])
						maxEmptyPages = 10
					}
					i++
				}
			case "--page-token", "-p":
				if i+1 < len(os.Args) {
					pageToken = os.Args[i+1]
					i++
				}
			case "--json", "-j":
				jsonOutput = true
			}
		}

		// Set custom config path if provided
		if configPath != "" {
			backend.ConfigPath = configPath
			}

			// Run list
			err := runCLIList(pageToken, pages, maxEmptyPages, jsonOutput)
			if err != nil {
				fmt.Fprintf(os.Stderr, "List failed: %v\n", err)
				os.Exit(1)
			}

	case "albums":
		// Check for help flag first
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printAlbumsHelp()
			return
		}

		// Parse flags
		configPath := ""
		pages := 1 // Default to 1 page
		pageToken := ""
		jsonOutput := false

		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--config", "-c":
				if i+1 < len(os.Args) {
					configPath = os.Args[i+1]
					i++
				}
			case "--pages":
				if i+1 < len(os.Args) {
					_, err := fmt.Sscanf(os.Args[i+1], "%d", &pages)
					if err != nil || pages < 1 {
						fmt.Fprintf(os.Stderr, "Warning: invalid pages value '%s', using 1\n", os.Args[i+1])
						pages = 1
					}
					i++
				}
			case "--page-token":
				if i+1 < len(os.Args) {
					pageToken = os.Args[i+1]
					i++
				}
			case "--json", "-j":
				jsonOutput = true
			default:
				fmt.Fprintf(os.Stderr, "Warning: unknown flag '%s'\n", os.Args[i])
			}
		}

		if configPath != "" {
			backend.ConfigPath = configPath
		}

		err := backend.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}

		mediaBrowser := &backend.MediaBrowser{}
		currentPageToken := pageToken

		for page := 0; page < pages; page++ {
			result, err := mediaBrowser.GetAlbumList(currentPageToken)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to get album list: %v\n", err)
				os.Exit(1)
			}

			if jsonOutput {
				fmt.Printf("%+v\n", result)
			} else {
				if page == 0 {
					fmt.Printf("Albums (Page %d):\n", page+1)
				} else {
					fmt.Printf("\nAlbums (Page %d):\n", page+1)
				}
				for i, album := range result.Albums {
					fmt.Printf("%d. %s\n", i+1, album.Title)
					fmt.Printf("   Key: %s\n", album.AlbumKey)
					if album.MediaCount > 0 {
						fmt.Printf("   Media Count: %d\n", album.MediaCount)
					}
				}
				fmt.Printf("\nTotal albums on this page: %d\n", len(result.Albums))
				if result.NextPageToken != "" {
					fmt.Printf("Next page token: %s\n", result.NextPageToken)
				}
			}

			// Check if there are more pages
			if result.NextPageToken == "" {
				if !jsonOutput && page+1 < pages {
					fmt.Println("\nNo more pages available.")
				}
				break
			}

			currentPageToken = result.NextPageToken
		}

	case "autowash":
		// Check for help
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printAutoWashHelp()
			return
		}

		// Defaults
		config := backend.AutoWashConfig{
			Interval:      1 * time.Hour,
			DbPath:        "media_db.json",
			BackupDir:     "Downloads/gotohp_backup",
			RetentionDays: 7,
		}
		
		if home, err := os.UserHomeDir(); err == nil {
			config.BackupDir = filepath.Join(home, "Downloads", "gotohp_backup")
		}

		configPath := ""

		// Parse flags
		for i := 2; i < len(os.Args); i++ {
			switch os.Args[i] {
			case "--interval", "-i":
				if i+1 < len(os.Args) {
					d, err := time.ParseDuration(os.Args[i+1])
					if err == nil {
						config.Interval = d
					} else {
						fmt.Fprintf(os.Stderr, "Invalid interval '%s', using default 1h\n", os.Args[i+1])
					}
					i++
				}
			case "--db":
				if i+1 < len(os.Args) {
					config.DbPath = os.Args[i+1]
					i++
				}
			case "--backup-dir":
				if i+1 < len(os.Args) {
					config.BackupDir = os.Args[i+1]
					i++
				}
			case "--retention", "-r":
				if i+1 < len(os.Args) {
					fmt.Sscanf(os.Args[i+1], "%d", &config.RetentionDays)
					i++
				}
			case "--config", "-c":
				if i+1 < len(os.Args) {
					configPath = os.Args[i+1]
					i++
				}
			}
		}

		if configPath != "" {
			backend.ConfigPath = configPath
		}
		
		// Load config (credentials needed)
		if err := backend.LoadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}

		if err := backend.RunAutoWash(config); err != nil {
			fmt.Fprintf(os.Stderr, "Auto-wash service error: %v\n", err)
			os.Exit(1)
		}

	case "login":
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printLoginHelp()
			return
		}
		configPath := ""
		tokenFile := ""
		fromStdin := false
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			switch {
			case arg == "--config" || arg == "-c":
				if i+1 >= len(os.Args) {
					fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
					os.Exit(1)
				}
				configPath = os.Args[i+1]
				i++
			case arg == "--token-file":
				if i+1 >= len(os.Args) {
					fmt.Fprintf(os.Stderr, "Error: --token-file requires a path\n")
					os.Exit(1)
				}
				tokenFile = os.Args[i+1]
				i++
			case arg == "--stdin" || arg == "-":
				fromStdin = true
			case strings.HasPrefix(arg, "-"):
				fmt.Fprintf(os.Stderr, "Error: unknown flag '%s'\n", arg)
				printLoginHelp()
				os.Exit(1)
			default:
				fmt.Fprintf(os.Stderr, "Error: unexpected positional argument %q\n", arg)
				fmt.Fprintln(os.Stderr, "The oauth_token must NOT be passed on the command line")
				fmt.Fprintln(os.Stderr, "(it would leak into shell history and process listings).")
				printLoginHelp()
				os.Exit(1)
			}
		}
		if configPath != "" {
			backend.ConfigPath = configPath
		}
		if err := backend.LoadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
		oauthToken, err := readOAuthToken(tokenFile, fromStdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Could not read oauth_token: %v\n", err)
			os.Exit(1)
		}
		if err := runCLILogin(oauthToken); err != nil {
			fmt.Fprintf(os.Stderr, "Login failed: %v\n", err)
			os.Exit(1)
		}

	case "empty-trash", "empty":
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printEmptyTrashHelp()
			return
		}
		configPath := ""
		yes := false
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			switch arg {
			case "--config", "-c":
				if i+1 >= len(os.Args) {
					fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
					os.Exit(1)
				}
				configPath = os.Args[i+1]
				i++
			case "--yes", "-y":
				yes = true
			default:
				fmt.Fprintf(os.Stderr, "Error: unknown argument '%s'\n", arg)
				printEmptyTrashHelp()
				os.Exit(1)
			}
		}
		if configPath != "" {
			backend.ConfigPath = configPath
		}
		if err := backend.LoadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
		if err := runCLIEmptyTrash(yes); err != nil {
			fmt.Fprintf(os.Stderr, "Empty-trash failed: %v\n", err)
			os.Exit(1)
		}

	case "trash", "delete", "purge":
		hard := command == "delete" || command == "purge"
		if len(os.Args) > 2 && (os.Args[2] == "--help" || os.Args[2] == "-h") {
			printDeleteHelp(hard)
			return
		}
		if len(os.Args) < 3 {
			fmt.Println("Error: mediaKey(s) required")
			printDeleteHelp(hard)
			os.Exit(1)
		}
		var mediaKeys []string
		configPath := ""
		for i := 2; i < len(os.Args); i++ {
			arg := os.Args[i]
			switch {
			case arg == "--config" || arg == "-c":
				if i+1 >= len(os.Args) {
					fmt.Fprintf(os.Stderr, "Error: %s requires a value\n", arg)
					os.Exit(1)
				}
				configPath = os.Args[i+1]
				i++
			case strings.HasPrefix(arg, "-"):
				fmt.Fprintf(os.Stderr, "Error: unknown flag '%s'\n", arg)
				printDeleteHelp(hard)
				os.Exit(1)
			default:
				mediaKeys = append(mediaKeys, arg)
			}
		}
		if len(mediaKeys) == 0 {
			fmt.Println("Error: at least one mediaKey required")
			os.Exit(1)
		}
		if configPath != "" {
			backend.ConfigPath = configPath
		}
		if err := backend.LoadConfig(); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
			os.Exit(1)
		}
		if err := runCLIDelete(mediaKeys, hard); err != nil {
			fmt.Fprintf(os.Stderr, "Delete failed: %v\n", err)
			os.Exit(1)
		}

	case "credentials", "creds":
		if len(os.Args) < 3 {
			fmt.Println("Error: subcommand required")
			printCredentialsHelp()
			os.Exit(1)
		}
		// Parse config flag before handling credentials
		var configPath string
		args := os.Args[2:]
		for i := 0; i < len(args); i++ {
			if args[i] == "--config" || args[i] == "-c" {
				if i+1 < len(args) {
					configPath = args[i+1]
					// Remove config flag from args
					args = append(args[:i], args[i+2:]...)
					break
				}
			}
		}
		if configPath != "" {
			backend.ConfigPath = configPath
		}
		handleCredentialsCommand(args)

	case "help", "--help", "-h":
		printCLIHelp()
	case "version", "--version", "-v":
		fmt.Printf("gotohp v%s\n", getAppVersion())
	default:
		fmt.Printf("Error: unknown command '%s'\n\n", command)
		printCLIHelp()
		os.Exit(1)
	}
}

func containsSubstring(str, substr string) bool {
	// Case-insensitive substring search
	strLower := strings.ToLower(str)
	substrLower := strings.ToLower(substr)
	return strings.Contains(strLower, substrLower)
}

func printCLIHelp() {
	fmt.Println(titleStyle.Render("gotohp - Unofficial Google Photos CLI & GUI"))
	fmt.Println()
	fmt.Println("Usage:")
	if cliHasGUI {
		fmt.Printf("  %s              Launch GUI application\n", commandStyle.Render(cliExecutableName))
	}
	fmt.Printf("  %s %s    Run CLI command\n", commandStyle.Render(cliExecutableName), argStyle.Render("<command>"))
	fmt.Println()
	fmt.Println("Core Commands:")
	fmt.Printf("  %s          Upload files or directories to Google Photos\n", commandStyle.Render("upload"))
	fmt.Printf("  %s        Download a file from Google Photos by media key\n", commandStyle.Render("download"))
	fmt.Printf("  %s       List media items in your library\n", commandStyle.Render("list, ls"))
	fmt.Printf("  %s          List your albums\n", commandStyle.Render("albums"))
	fmt.Println()
	fmt.Println("Advanced Commands:")
	fmt.Printf("  %s          Add a credential from a browser oauth_token (zero ADB)\n", commandStyle.Render("login"))
	fmt.Printf("  %s          Move media to trash by mediaKey\n", commandStyle.Render("trash"))
	fmt.Printf("  %s         Permanently delete media by mediaKey\n", commandStyle.Render("delete"))
	fmt.Printf("  %s    Permanently delete every item currently in trash\n", commandStyle.Render("empty-trash"))
	fmt.Printf("  %s       Manage Google Photos credentials/accounts\n", commandStyle.Render("creds"))
	fmt.Printf("  %s       Start auto-sync and backup service\n", commandStyle.Render("autowash"))
	fmt.Printf("  %s   Download a thumbnail (various sizes available)\n", commandStyle.Render("thumbnail"))
	fmt.Println()
	fmt.Println("System Commands:")
	fmt.Printf("  %s            Show this help message\n", commandStyle.Render("help, -h"))
	fmt.Printf("  %s         Show version information\n", commandStyle.Render("version, -v"))
	fmt.Println()
	fmt.Println("Options:")
	fmt.Printf("  Use '%s <command> --help' for detailed information on any command.\n", cliExecutableName)
}

func printFlag(short, long, arg, description string) {
	var flagPart string
	if short != "" {
		flagPart = fmt.Sprintf("  %s, %s", flagStyle.Render(short), flagStyle.Render(long))
	} else {
		flagPart = fmt.Sprintf("      %s", flagStyle.Render(long))
	}
	
	if arg != "" {
		flagPart += " " + argStyle.Render(arg)
	}
	
	// Pad flagPart to a consistent width
	padding := 30 - lipgloss.Width(flagPart)
	if padding < 1 {
		padding = 1
	}
	
	fmt.Printf("%s%s%s\n", flagPart, strings.Repeat(" ", padding), description)
}

func printUploadHelp() {
	fmt.Printf("Usage: %s %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("upload"), argStyle.Render("<filepath>"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Upload files or directories to Google Photos.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-r", "--recursive", "", "Include subdirectories")
	printFlag("-t", "--threads", "<n>", "Number of upload threads (default: 3)")
	printFlag("-f", "--force", "", "Force upload even if file exists")
	printFlag("-d", "--delete", "", "Delete from host after upload")
	printFlag("-df", "--disable-filter", "", "Disable file type filtering")
	printFlag("", "--date-from-filename", "", "Set media date from filename (e.g. 20240709_182027.jpg)")
	printFlag("-e", "--exclude", "<pattern>", "Skip directories with this exact name during recursive upload")
	printFlag("-a", "--album", "<name>", "Add uploaded files to album (use AUTO for folder-based albums)")
	printFlag("-l", "--log-level", "<level>", "Set log level: debug, info, warn, error (default: info)")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func printAutoWashHelp() {
	fmt.Printf("Usage: %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("autowash"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Start auto-wash service to automatically sync library and backup media.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-i", "--interval", "<duration>", "Check interval (default: 1h, e.g. 30m, 2h)")
	printFlag("", "--db", "<path>", "Database file path (default: media_db.json)")
	printFlag("", "--backup-dir", "<path>", "Directory for temporary downloads (default: Downloads/gotohp_backup)")
	printFlag("-r", "--retention", "<days>", "Days to keep downloaded files (default: 7)")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func printDownloadHelp() {
	fmt.Printf("Usage: %s %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("download"), argStyle.Render("<media-key>"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Download a file from Google Photos by mediaKey.")
	fmt.Println("Use 'gotohp list -j' to see mediaKeys for items in your library.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-o", "--output", "<path>", "Output file path (default: original filename)")
	printFlag("", "--original", "", "Download the original file instead of edited")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func printThumbnailHelp() {
	fmt.Printf("Usage: %s %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("thumbnail"), argStyle.Render("<media-key>"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Download a thumbnail of a media item at various sizes.")
	fmt.Println()
	fmt.Println("Size Presets (--size, -s):")
	fmt.Printf("  %-10s - 50x50 pixels\n", argStyle.Render("small"))
	fmt.Printf("  %-10s - 800x800 pixels\n", argStyle.Render("medium"))
	fmt.Printf("  %-10s - 1600x1600 pixels\n", argStyle.Render("large"))
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-o", "--output", "<path>", "Output file path")
	printFlag("-s", "--size", "<preset>", "Use a size preset (small, medium, large)")
	printFlag("-w", "--width", "<pixels>", "Custom thumbnail width in pixels")
	printFlag("-h", "--height", "<pixels>", "Custom thumbnail height in pixels")
	printFlag("", "--overlay", "", "Include overlay (e.g., play symbol for videos)")
	printFlag("", "--png", "", "Get PNG format instead of JPEG")
	printFlag("-c", "--config", "<path>", "Path to config file")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Printf("  %s\n", exampleStyle.Render("gotohp thumbnail ABC123 --size medium"))
	fmt.Printf("  %s\n", exampleStyle.Render("gotohp thumbnail ABC123 -w 640 -h 480"))
}

func printListHelp() {
	fmt.Printf("Usage: %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("list"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("List media items in your Google Photos library.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("", "--pages", "<n>", "Number of pages to fetch (default: 1)")
	printFlag("", "--max-empty-pages", "<n>", "Max consecutive empty pages to skip (default: 10)")
	printFlag("-p", "--page-token", "<t>", "Page token for pagination")
	printFlag("-j", "--json", "", "Output in JSON format")
	printFlag("-c", "--config", "<path>", "Path to config file")
	fmt.Println()
	fmt.Println("Note: If a page returns 0 items, the next page will be fetched automatically.")
}

func printAlbumsHelp() {
	fmt.Printf("Usage: %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("albums"), flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("List albums in your Google Photos library.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("", "--pages", "<n>", "Number of pages to fetch (default: 1)")
	printFlag("", "--page-token", "<t>", "Page token for pagination")
	printFlag("-j", "--json", "", "Output in JSON format")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func printCredentialsHelp() {
	fmt.Printf("Usage: %s %s %s %s\n", commandStyle.Render(cliExecutableName), commandStyle.Render("creds"), argStyle.Render("<subcommand>"), flagStyle.Render("[args]"))
	fmt.Println()
	fmt.Println("Manage Google Photos credentials and accounts.")
	fmt.Println()
	fmt.Println("Subcommands:")
	
	printSubcommand := func(cmd, arg, desc string) {
		fullCmd := commandStyle.Render(cmd)
		if arg != "" {
			fullCmd += " " + argStyle.Render(arg)
		}
		padding := 30 - lipgloss.Width(fullCmd)
		if padding < 1 {
			padding = 1
		}
		fmt.Printf("  %s%s%s\n", fullCmd, strings.Repeat(" ", padding), desc)
	}

	printSubcommand("add", "<auth-string>", "Add a new credential")
	printSubcommand("remove, rm", "<email>", "Remove a credential by email")
	printSubcommand("list, ls", "", "List all stored credentials")
	printSubcommand("set, select", "<email>", "Set active credential")
	
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func handleCredentialsCommand(args []string) {
	if len(args) == 0 {
		printCredentialsHelp()
		os.Exit(1)
	}

	// Load config
	err := backend.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	configManager := &backend.ConfigManager{}
	subcommand := args[0]

	switch subcommand {
	case "add":
		if len(args) < 2 {
			fmt.Println("Error: auth-string required")
			fmt.Printf("Usage: %s credentials add <auth-string>\n", cliExecutableName)
			os.Exit(1)
		}
		authString := args[1]
		err := configManager.AddCredentials(authString)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error adding credentials: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("✓ Credentials added successfully")

	case "remove", "rm":
		if len(args) < 2 {
			fmt.Println("Error: email required")
			fmt.Printf("Usage: %s credentials remove <email>\n", cliExecutableName)
			os.Exit(1)
		}
		email := args[1]
		err := configManager.RemoveCredentials(email)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error removing credentials: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Credentials for %s removed successfully\n", email)

	case "list", "ls":
		config := configManager.GetConfig()
		if len(config.Credentials) == 0 {
			fmt.Println("No credentials found")
			return
		}
		fmt.Println("Credentials:")
		for i, cred := range config.Credentials {
			params, err := backend.ParseAuthString(cred)
			if err != nil {
				fmt.Printf("  %d. [Invalid credential]\n", i+1)
				continue
			}
			email := params.Get("Email")
			marker := " "
			if email == config.Selected {
				marker = "*"
			}
			fmt.Printf("  %s %s\n", marker, email)
		}
		if config.Selected != "" {
			fmt.Printf("\n* = active\n")
		}
		fmt.Printf("\nUse 'gotohp creds set <email>' to change active account (supports partial matching)\n")

	case "set", "select":
		if len(args) < 2 {
			fmt.Println("Error: email required")
			fmt.Printf("Usage: %s creds set <email>\n", cliExecutableName)
			os.Exit(1)
		}
		query := args[1]
		config := configManager.GetConfig()

		// Try to find exact match first
		var matchedEmail string
		for _, cred := range config.Credentials {
			params, err := backend.ParseAuthString(cred)
			if err != nil {
				continue
			}
			email := params.Get("Email")
			if email == query {
				matchedEmail = email
				break
			}
		}

		// If no exact match, try fuzzy matching (substring match)
		if matchedEmail == "" {
			var candidates []string
			for _, cred := range config.Credentials {
				params, err := backend.ParseAuthString(cred)
				if err != nil {
					continue
				}
				email := params.Get("Email")
				// Check if query is a substring of the email
				if containsSubstring(email, query) {
					candidates = append(candidates, email)
				}
			}

			if len(candidates) == 0 {
				fmt.Fprintf(os.Stderr, "Error: no credentials found matching '%s'\n", query)
				os.Exit(1)
			} else if len(candidates) == 1 {
				matchedEmail = candidates[0]
			} else {
				fmt.Fprintf(os.Stderr, "Error: multiple credentials match '%s':\n", query)
				for _, email := range candidates {
					fmt.Fprintf(os.Stderr, "  - %s\n", email)
				}
				fmt.Fprintf(os.Stderr, "Please be more specific\n")
				os.Exit(1)
			}
		}

		configManager.SetSelected(matchedEmail)
		fmt.Printf("✓ Active credential set to %s\n", matchedEmail)

	default:
		fmt.Printf("Error: unknown subcommand '%s'\n\n", subcommand)
		printCredentialsHelp()
		os.Exit(1)
	}
}

// runCLILogin turns an oauth_token cookie from an EmbeddedSetup browser login
// into a stored credential. No Android device involved.
func runCLILogin(oauthToken string) error {
	androidID, err := backend.RandomAndroidID()
	if err != nil {
		return fmt.Errorf("androidId gen: %w", err)
	}
	fmt.Println("Exchanging oauth_token for master token...")
	email, master, err := backend.ExchangeOAuthToken(oauthToken, androidID)
	if err != nil {
		return err
	}
	authString := backend.BuildAuthStringFromMasterToken(email, master, androidID)

	cm := &backend.ConfigManager{}
	previouslySelected := strings.TrimSpace(backend.AppConfig.Selected)
	if err := cm.AddCredentials(authString); err != nil {
		return fmt.Errorf("save credential: %w", err)
	}
	// If there was no active account yet, select the new one automatically.
	// Otherwise leave the existing selection alone and tell the user, so that
	// adding a second account never silently redirects subsequent commands to
	// the wrong mailbox.
	if previouslySelected == "" {
		cm.SetSelected(email)
	}
	fmt.Printf("✓ Logged in as %s\n", email)
	active := strings.TrimSpace(backend.AppConfig.Selected)
	if active == "" {
		active = email
	}
	if active != email {
		fmt.Printf("  Active account is still %s\n", active)
		fmt.Printf("  Switch with: %s creds set %s\n", cliExecutableName, email)
	} else {
		fmt.Printf("  Active account: %s\n", active)
	}
	// Deliberately NOT printing master token or androidId.
	return nil
}

// readOAuthToken pulls the oauth_token from one of three sources, in priority:
//   1. --token-file <path>      (recommended; not visible in `ps`)
//   2. env GOTOHP_OAUTH_TOKEN   (script-friendly)
//   3. stdin                    (interactive default; also --stdin/-)
// A CLI positional argument is intentionally NOT accepted: it would leak into
// shell history, /proc/<pid>/cmdline, and `ps` output.
func readOAuthToken(file string, stdin bool) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", file, err)
		}
		tok := strings.TrimSpace(string(b))
		if tok == "" {
			return "", fmt.Errorf("token file is empty")
		}
		return tok, nil
	}
	if tok := strings.TrimSpace(os.Getenv("GOTOHP_OAUTH_TOKEN")); tok != "" {
		return tok, nil
	}
	if !stdin {
		fmt.Fprintln(os.Stderr, "Paste the oauth_token cookie value and press Enter.")
		fmt.Fprintln(os.Stderr, "(Or re-run with --token-file <path> or GOTOHP_OAUTH_TOKEN env var.)")
		fmt.Fprint(os.Stderr, "oauth_token: ")
	}
	// Use a bufio.Scanner with an enlarged buffer so tokens containing
	// unusual characters or unusual length are not silently truncated (as
	// fmt.Fscanln would). Read exactly one line, verbatim.
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return "", fmt.Errorf("no token provided")
	}
	line := strings.TrimSpace(scanner.Text())
	if line == "" {
		return "", fmt.Errorf("empty token")
	}
	return line, nil
}

// runCLIDelete accepts mediaKeys from the user, resolves each to its
// server-authoritative dedupKey, then trashes or permanently deletes. Batches
// of >100 are chunked to stay under any server-side request-size limit.
func runCLIDelete(mediaKeys []string, hard bool) error {
	if len(mediaKeys) == 0 {
		return fmt.Errorf("nothing to delete")
	}
	fmt.Printf("Resolving %d mediaKey(s) → dedupKeys via GetMediaInfo...\n", len(mediaKeys))
	pairs, err := resolveDedupKeysFromMediaKeys(mediaKeys)
	if err != nil {
		return err
	}
	if len(pairs) == 0 {
		return fmt.Errorf("no valid dedup keys resolved")
	}
	dedupKeys := make([]string, len(pairs))
	for i, p := range pairs {
		dedupKeys[i] = p.DedupKey
	}
	api, err := backend.NewApi()
	if err != nil {
		return err
	}
	const chunk = 100
	trashInBatches := func(action string, fn func([]string) error) error {
		for i := 0; i < len(dedupKeys); i += chunk {
			end := i + chunk
			if end > len(dedupKeys) {
				end = len(dedupKeys)
			}
			batch := dedupKeys[i:end]
			if err := fn(batch); err != nil {
				return fmt.Errorf("%s batch %d-%d: %w", action, i, end, err)
			}
			if len(dedupKeys) > chunk {
				fmt.Printf("  %s %d / %d\n", action, end, len(dedupKeys))
			}
		}
		return nil
	}
	if hard {
		fmt.Printf("Moving %d item(s) to trash first...\n", len(dedupKeys))
		if err := trashInBatches("trash", api.MoveToTrash); err != nil {
			return err
		}
		fmt.Println("Permanently deleting...")
		if err := trashInBatches("purge", api.PermanentlyDelete); err != nil {
			return fmt.Errorf("permanent delete (items are still in trash): %w", err)
		}
	} else {
		fmt.Printf("Moving %d item(s) to trash...\n", len(dedupKeys))
		if err := trashInBatches("trash", api.MoveToTrash); err != nil {
			return err
		}
	}
	action := "trashed"
	if hard {
		action = "permanently deleted"
	}
	fmt.Printf("✓ %d item(s) %s\n", len(pairs), action)
	for _, p := range pairs {
		fmt.Printf("  - %s\n", p.MediaKey)
	}
	return nil
}

// resolvedKey pairs the mediaKey the user supplied with the server-authoritative
// dedupKey required by MoveToTrash / PermanentlyDelete.
type resolvedKey struct {
	MediaKey string
	DedupKey string
}

// resolveDedupKeysFromMediaKeys converts a list of mediaKeys into
// (mediaKey, dedupKey) pairs via GetMediaInfo. This is the only sound way to
// feed the destructive endpoints without trusting local assumptions about how
// Google derives dedupKey from file bytes. Empty and duplicate inputs are
// dropped; the returned slice reflects exactly what will be operated on.
func resolveDedupKeysFromMediaKeys(mediaKeys []string) ([]resolvedKey, error) {
	api, err := backend.NewApi()
	if err != nil {
		return nil, err
	}
	out := make([]resolvedKey, 0, len(mediaKeys))
	seen := map[string]struct{}{}
	for _, mk := range mediaKeys {
		mk = strings.TrimSpace(mk)
		if mk == "" {
			continue
		}
		if _, dup := seen[mk]; dup {
			continue
		}
		seen[mk] = struct{}{}
		info, err := api.GetMediaInfo(mk)
		if err != nil {
			return nil, fmt.Errorf("resolve mediaKey %s: %w", mk, err)
		}
		if info == nil || info.DedupKey == "" {
			return nil, fmt.Errorf("mediaKey %s has no dedup key in server response", mk)
		}
		out = append(out, resolvedKey{MediaKey: mk, DedupKey: info.DedupKey})
	}
	return out, nil
}

func printLoginHelp() {
	fmt.Printf("Usage: %s %s %s\n",
		commandStyle.Render(cliExecutableName),
		commandStyle.Render("login"),
		flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Add a Google Photos credential without an Android device.")
	fmt.Println()
	fmt.Println("Steps:")
	fmt.Println("  1. In a private/incognito browser window, open:")
	fmt.Println("       https://accounts.google.com/EmbeddedSetup")
	fmt.Println("  2. Sign in normally (2FA / passkey all supported).")
	fmt.Println("  3. After sign-in completes, open DevTools:")
	fmt.Println("       Application → Cookies → https://accounts.google.com")
	fmt.Println("     Copy the value of the cookie named 'oauth_token'.")
	fmt.Println("  4. Provide it via one of (in priority order):")
	fmt.Printf("       %s login --token-file /path/to/token.txt\n", cliExecutableName)
	fmt.Printf("       GOTOHP_OAUTH_TOKEN=oauth2_4/... %s login\n", cliExecutableName)
	fmt.Printf("       %s login          (prompts on stdin)\n", cliExecutableName)
	fmt.Println()
	fmt.Println("The token is NEVER accepted as a command-line argument to")
	fmt.Println("prevent it leaking into shell history and `ps` output.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("", "--token-file", "<path>", "Read oauth_token from this file")
	printFlag("", "--stdin", "", "Read oauth_token from stdin (default when nothing else set)")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

func printDeleteHelp(hard bool) {
	verb := "trash"
	desc := "Move media items to trash (soft delete, recoverable within 60 days)."
	if hard {
		verb = "delete"
		desc = "Permanently delete media items from Google Photos. This cannot be undone."
	}
	fmt.Printf("Usage: %s %s %s %s\n",
		commandStyle.Render(cliExecutableName),
		commandStyle.Render(verb),
		argStyle.Render("<media-key>..."),
		flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println(desc)
	fmt.Println()
	fmt.Println("Arguments must be mediaKeys (from 'gotohp list -j' output).")
	fmt.Println("The dedup key required by the server is resolved automatically")
	fmt.Println("via GetMediaInfo, so you never touch it directly.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-c", "--config", "<path>", "Path to config file")
}

// runCLIEmptyTrash pulls every trashed item via list, then hard-deletes them
// by dedup key. Prompts for confirmation unless --yes is set.
//
// NOTE: This temporarily flips the process-global
// backend.AppConfig.RequestTrashItems flag so that the list request includes
// trashed items regardless of the user's persisted setting. That flag is
// unfortunately read (unlocked) inside the protobuf request builder in
// backend/api.go, so plumbing a per-call override would touch the whole
// media-list request stack. In the CLI runtime this is safe: the command runs
// synchronously with no other goroutines touching the flag. If we ever call
// this from a context where a background scan can race, the flag needs to
// become a per-request parameter instead.
func runCLIEmptyTrash(assumeYes bool) error {
	prev := backend.AppConfig.RequestTrashItems
	backend.AppConfig.RequestTrashItems = true
	defer func() { backend.AppConfig.RequestTrashItems = prev }()

	mb := &backend.MediaBrowser{}
	seen := map[string]struct{}{}
	var dedupKeys []string
	pageToken := ""
	scanned := 0

	fmt.Println("Scanning library for trashed items...")
	// Terminate strictly on empty NextPageToken. No streak heuristic: on large
	// libraries with sparsely-interleaved trashed items an early-stop can miss
	// tail matches, and we are about to permanently delete.
	//
	// Safety rails: hard iteration cap so a buggy server returning a
	// non-advancing NextPageToken cannot spin forever, and detect a stuck
	// token (identical page returned twice) since we are one call away from
	// a destructive operation.
	const maxPages = 2000
	prevToken := ""
	for iter := 0; iter < maxPages; iter++ {
		res, err := mb.GetMediaList(pageToken, "", 0, 500)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		if res == nil {
			return fmt.Errorf("list: unexpected nil result")
		}
		scanned += len(res.Items)
		for _, it := range res.Items {
			if !it.IsTrash || it.DedupKey == "" {
				continue
			}
			if _, ok := seen[it.DedupKey]; ok {
				continue
			}
			seen[it.DedupKey] = struct{}{}
			dedupKeys = append(dedupKeys, it.DedupKey)
		}
		if res.NextPageToken == "" {
			break
		}
		if res.NextPageToken == prevToken {
			return fmt.Errorf("pagination stalled: server returned the same NextPageToken twice; aborting to avoid a partial trash purge")
		}
		prevToken = pageToken
		pageToken = res.NextPageToken
		if iter == maxPages-1 {
			return fmt.Errorf("pagination exceeded %d iterations without terminating; aborting", maxPages)
		}
	}
	fmt.Printf("Scanned %d items total.\n", scanned)

	if len(dedupKeys) == 0 {
		fmt.Println("Trash is already empty.")
		return nil
	}
	fmt.Printf("Found %d item(s) in trash.\n", len(dedupKeys))

	if !assumeYes {
		fmt.Print("Permanently delete all of them? This CANNOT be undone. [y/N]: ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	api, err := backend.NewApi()
	if err != nil {
		return err
	}
	// Chunk deletes: the endpoint accepts hundreds per call but stay conservative.
	const chunk = 100
	deleted := 0
	for i := 0; i < len(dedupKeys); i += chunk {
		end := i + chunk
		if end > len(dedupKeys) {
			end = len(dedupKeys)
		}
		batch := dedupKeys[i:end]
		if err := api.PermanentlyDelete(batch); err != nil {
			return fmt.Errorf("permanently delete (batch %d-%d): %w", i, end, err)
		}
		deleted += len(batch)
		fmt.Printf("  purged %d / %d\n", deleted, len(dedupKeys))
	}
	fmt.Printf("✓ Trash emptied: %d item(s) permanently deleted.\n", deleted)
	return nil
}

func printEmptyTrashHelp() {
	fmt.Printf("Usage: %s %s %s\n",
		commandStyle.Render(cliExecutableName),
		commandStyle.Render("empty-trash"),
		flagStyle.Render("[flags]"))
	fmt.Println()
	fmt.Println("Scans the library for items already in trash and permanently deletes")
	fmt.Println("all of them. This CANNOT be undone.")
	fmt.Println()
	fmt.Println("Flags:")
	printFlag("-y", "--yes", "", "Skip confirmation prompt")
	printFlag("-c", "--config", "<path>", "Path to config file")
}
