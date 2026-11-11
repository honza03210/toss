import AVFoundation
import SwiftUI

/// A sheet with a live camera view that reports the first QR code it sees.
struct ScannerSheet: View {
    let onCode: (String) -> Void
    @Environment(\.dismiss) private var dismiss
    @State private var denied = false

    var body: some View {
        NavigationStack {
            ZStack {
                if denied {
                    VStack(spacing: 12) {
                        Image(systemName: "camera.fill").font(.largeTitle)
                        Text("Toss needs the camera to scan the pairing code.").multilineTextAlignment(.center)
                        Text("You can also scan it with the Camera app, or allow the camera in Settings.")
                            .font(.footnote).foregroundStyle(.secondary).multilineTextAlignment(.center)
                        Button("Open Settings") { UIApplication.shared.open(URL(string: UIApplication.openSettingsURLString)!) }
                    }
                    .padding()
                } else {
                    ScannerView(onCode: onCode).ignoresSafeArea()
                    VStack {
                        Spacer()
                        Text("Point the camera at the code from `toss pair`")
                            .font(.callout).padding(10)
                            .background(.ultraThinMaterial, in: Capsule())
                            .padding(.bottom, 30)
                    }
                }
            }
            .navigationTitle("Scan pairing code")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
            }
        }
        .task {
            switch AVCaptureDevice.authorizationStatus(for: .video) {
            case .notDetermined:
                denied = !(await AVCaptureDevice.requestAccess(for: .video))
            case .authorized:
                denied = false
            default:
                denied = true
            }
        }
    }
}

struct ScannerView: UIViewControllerRepresentable {
    let onCode: (String) -> Void

    func makeUIViewController(context: Context) -> ScannerController {
        let controller = ScannerController()
        controller.onCode = onCode
        return controller
    }

    func updateUIViewController(_ controller: ScannerController, context: Context) {}
}

final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: ((String) -> Void)?
    private let session = AVCaptureSession()
    private var preview: AVCaptureVideoPreviewLayer?
    private var reported = false
    private let sessionQueue = DispatchQueue(label: "Toss.scanner")

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        guard let device = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: device),
              session.canAddInput(input) else { return }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { return }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.videoGravity = .resizeAspectFill
        view.layer.addSublayer(preview)
        self.preview = preview
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        preview?.frame = view.bounds
    }

    override func viewWillAppear(_ animated: Bool) {
        super.viewWillAppear(animated)
        let session = self.session
        sessionQueue.async { session.startRunning() }
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        let session = self.session
        sessionQueue.async { session.stopRunning() }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard !reported, let code = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        reported = true
        UIImpactFeedbackGenerator(style: .medium).impactOccurred()
        onCode?(code)
    }
}
