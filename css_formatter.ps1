$files = Get-ChildItem -Path e:\Code\api-monitor\src\css -Filter *.css -Recurse
foreach ($f in $files) {
    if ($f.Name -eq "styles.css") {
        # Optional: We still process styles.css because it has many class definitions
    }
    
    $content = Get-Content $f.FullName -Raw
    if (-not $content) { continue }
    
    # Margin, Padding, Gap, Positional
    $content = [regex]::Replace($content, '(?mi)^(\s*(?:margin|padding|gap|top|bottom|left|right)[a-z-]*:\s*)(.*?)(;)', {
        param($m)
        $prefix = $m.Groups[1].Value
        $vals = $m.Groups[2].Value
        $suffix = $m.Groups[3].Value
        
        if ($vals -notmatch 'var\(--') {
            $vals = $vals -replace '\b(4|5)px\b', 'var(--space-xs)'
            $vals = $vals -replace '\b(8|10)px\b', 'var(--space-sm)'
            $vals = $vals -replace '\b(12)px\b', 'var(--space-md)'
            $vals = $vals -replace '\b(14|15|16)px\b', 'var(--space-lg)'
            $vals = $vals -replace '\b(20|24|25)px\b', 'var(--space-xl)'
        }
        
        return $prefix + $vals + $suffix
    })

    # Border Radius
    $content = [regex]::Replace($content, '(?mi)^(\s*border-radius:\s*)(.*?)(;)', {
        param($m)
        $prefix = $m.Groups[1].Value
        $vals = $m.Groups[2].Value
        $suffix = $m.Groups[3].Value
        
        if ($vals -notmatch 'var\(--') {
            $vals = $vals -replace '\b(2|3|4|5|6)px\b', 'var(--radius-sm)'
            $vals = $vals -replace '\b(8|10)px\b', 'var(--radius-md)'
            $vals = $vals -replace '\b(12|14|16|18|20|24)px\b', 'var(--radius-lg)'
            $vals = $vals -replace '\b(99|999|9999)px\b', 'var(--radius-full)'
        }
        
        return $prefix + $vals + $suffix
    })

    Set-Content -Path $f.FullName -Value $content -NoNewline -Encoding utf8
}
Write-Host "Replacement Complete."
