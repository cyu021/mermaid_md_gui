Mermaid Mindmap GUI Editor
==========================

A powerful, interactive GUI editor for Mermaid-style mindmaps, built with Go and Fyne.

Supported Features
------------------

1. Multiple Layout Modes:
   - Balanced Mindmap: Symmetrical distribution around the root.
   - Fishbone (Ishikawa): Ideal for cause-and-effect analysis.
   - Logic (Left): Hierarchy extending to the left.
   - Logic (Right): Hierarchy extending to the right.

2. Customizable Route Styles:
   - Bezier: Smooth, elegant cubic curves (S-curves).
   - Oval: Arced connections for a modern, rounded look.
   - Orthogonal: Clean, right-angled stepwise lines.

3. Visual Customization (via Settings):
   - Adjustable Line Thickness (1px - 10px).
   - Adjustable Node Padding (5px - 50px).
   - Automatic "Halo" rendering to ensure route visibility over node boundaries.

4. Interactive Canvas:
   - Real-time Preview: See changes instantly as you type.
   - Node Collapsing: Toggle branches using the "+" and "-" icons.
   - Zooming: Fluid Zoom In and Zoom Out support.
   - Mini-Map: Quick navigation for large diagrams (bottom-right).
   - Auto-Centering: One-click "Refresh & Center" to reset the view.

5. Export Options:
   - Export as PNG: High-quality image export with a print-friendly white background (saves ink).

6. Unicode & CJK Support:
   - Built-in font fallback for Chinese, Japanese, and Korean characters.


How to Use
----------

1. Launching the Application:
   - Run via Go: `go run main.go`
   - Or execute the compiled binary: `./mermaid-md-gui` (Linux) or `mermaid-md-gui.exe` (Windows).

2. Editing the Mindmap:
   - Type your mindmap structure in the left text panel.
   - Use indentation (spaces or tabs) to define hierarchy.

   Example Syntax:
   ```mermaid
   mindmap
     root((Root Node))
       Branch A
         Sub-branch 1
         Sub-branch 2
       Branch B
         Sub-branch 3
   ```

3. Using the Toolbar:
   - Layout Select: Switch between Balanced, Fishbone, and Logic modes.
   - Route Select: Change the connection line style (Bezier/Oval/Orthogonal).
   - Settings: Open the sliders to adjust line thickness and padding.
   - Refresh: Reset the canvas position and re-calculate the layout.

4. Exporting:
   - Go to "File" -> "Export as PNG" to save your diagram for presentations or printing.

5. UI Scaling (FYNE_SCALE):
   If the automatic scaling detection does not suit your needs, you can manually override the interface size using the `FYNE_SCALE` environment variable.

   - **Linux / macOS**:
     ```bash
     FYNE_SCALE=1.5 ./mermaid-md-gui
     ```
   - **Windows (Command Prompt)**:
     ```cmd
     set FYNE_SCALE=1.5
     mermaid-md-gui.exe
     ```
   - **Windows (PowerShell)**:
     ```powershell
     $env:FYNE_SCALE = "1.5"
     .\mermaid-md-gui.exe
     ```
   *Note: A value of 1.0 is standard; 2.0 is double size; 0.5 is half size.*


System Requirements
-------------------
- Operating System: Windows, Linux, or macOS.
- Dependencies: Requires a C compiler (gcc) for Fyne/Go compilation.

Build Instructions
------------------

### 1. Linux (WSL/Native)
   - Prerequisites: `gcc`, `libgl1-mesa-dev`, `xorg-dev`.
   - Command (Optimized for resource management):
     ```bash
     NPROC=$(nproc); GOMAX=$((NPROC > 2 ? NPROC - 2 : 1)); \
     go build -v -p $GOMAX -o mermaid-md-gui main.go
     ```

### 2. Windows (Native)
   - Prerequisites: `gcc` (MinGW-w64).
   - Command:
     ```cmd
     go build -v -ldflags "-s -w -H windowsgui" -o mermaid-md-gui.exe main.go
     ```

### 3. macOS
   - Prerequisites: `Xcode` or Command Line Tools.
   - Command (Intel):
     ```bash
     go build -v -o mermaid-md-gui-mac main.go
     ```
   - Command (Apple Silicon):
     ```bash
     GOARCH=arm64 go build -v -o mermaid-md-gui-mac-arm main.go
     ```

### 4. Windows (Cross-Compilation from WSL/Linux)
   - Prerequisite: `x86_64-w64-mingw32-gcc`.
   - Command (Optimized and Static):
     ```bash
     NPROC=$(nproc); GOMAX=$((NPROC > 2 ? NPROC - 2 : 1)); \
     GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc \
     go build -v -p $GOMAX -ldflags="-s -w -extldflags=-static -H=windowsgui" \
     -o mermaid-md-gui.exe main.go
     ```
