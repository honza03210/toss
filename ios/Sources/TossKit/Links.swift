import Foundation

/// Where people get the computer companion (shown in the app). Update when the repository moves.
public enum Links {
    public static let website = URL(string: "https://github.com/OWNER/toss")!
    public static let installLinux = "curl -fsSL https://github.com/OWNER/toss/releases/latest/download/install.sh | sh"
    public static let installWindows = "irm https://github.com/OWNER/toss/releases/latest/download/install.ps1 | iex"
    public static let privacy = URL(string: "https://OWNER.github.io/toss/privacy.html")!
}
