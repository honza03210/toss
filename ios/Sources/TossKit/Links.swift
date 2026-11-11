import Foundation

/// Where people get the computer companion (shown in the app). Update when the repository moves.
public enum Links {
    public static let website = URL(string: "https://github.com/honza03210/toss")!
    public static let installLinux = "curl -fsSL https://github.com/honza03210/toss/releases/latest/download/install.sh | sh"
    public static let installWindows = "irm https://github.com/honza03210/toss/releases/latest/download/install.ps1 | iex"
    public static let privacy = URL(string: "https://honza03210.github.io/toss/privacy.html")!
}
