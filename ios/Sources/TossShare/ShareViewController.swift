import SwiftUI
import TossKit
import UIKit

/// The share extension's entry point (NSExtensionPrincipalClass): shows SendView, which sends
/// right away to the most recently used computer.
@objc(ShareViewController)
final class ShareViewController: UIViewController {
    override func viewDidLoad() {
        super.viewDidLoad()
        let providers = (extensionContext?.inputItems as? [NSExtensionItem] ?? []).flatMap { $0.attachments ?? [] }
        let model = SendModel { folder, convertHEIC in
            await ItemLoader.load(providers, into: folder, convertHEIC: convertHEIC)
        }
        model.onFinish = { [weak self] success in
            if success {
                self?.extensionContext?.completeRequest(returningItems: nil)
            } else {
                self?.extensionContext?.cancelRequest(withError: CocoaError(.userCancelled))
            }
        }
        let host = UIHostingController(rootView: SendView(model: model))
        addChild(host)
        host.view.frame = view.bounds
        host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        view.addSubview(host.view)
        host.didMove(toParent: self)
    }
}
